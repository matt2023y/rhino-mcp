using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.NetworkInformation;
using System.Net.Sockets;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading;
using System.Threading.Tasks;
using Rhino;
using Rhino.PlugIns;
using RhinoMcpPlugin.Capture;
using RhinoMcpPlugin.Exec;

namespace RhinoMcpPlugin
{
    [System.Runtime.InteropServices.Guid("4C452F68-5142-4E8A-8D9B-5934C7AE17B3")]
    public class RhinoMcpPlugin : PlugIn
    {
        private const int MaxRequestBodyBytes = 2 * 1024 * 1024;
        private const int MaxConcurrentConnections = 32;
        private const long MaxCapturePixels = 16_000_000;

        private readonly object _lifecycleGate = new object();
        private TcpListener _listener;
        private CancellationTokenSource _cts;
        private readonly SemaphoreSlim _operationGate = new SemaphoreSlim(1, 1);
        private readonly SemaphoreSlim _connectionGate = new SemaphoreSlim(MaxConcurrentConnections, MaxConcurrentConnections);
        private IPAddress[] _listenAddresses;
        private int _port;
        private string _token;
        private string _endpointFile;
        private string _replacedEndpointFile;
        private Task _listenerTask;
        private int _shutdownStarted;
        private UnhandledExceptionEventHandler _unhandledExceptionHandler;
        private EventHandler<UnobservedTaskExceptionEventArgs> _unobservedTaskExceptionHandler;

        public RhinoMcpPlugin() { Instance = this; }
        public static RhinoMcpPlugin Instance { get; private set; }

        public override PlugInLoadTime LoadTime => PlugInLoadTime.AtStartup;

        protected override LoadReturnCode OnLoad(ref string errorMessage)
        {
            try
            {
                _cts = new CancellationTokenSource();
                var profile = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile);
                EndpointConfiguration localConfig = null;
                try
                {
                    localConfig = EndpointConfiguration.LoadSaved(profile);
                }
                catch (Exception ex) when (ex is IOException || ex is UnauthorizedAccessException ||
                                           ex is InvalidDataException || ex is JsonException)
                {
                    // A missing, unreadable, or stale config must not prevent Rhino
                    // from loading the plugin; fall back to a dynamic endpoint.
                    RhinoApp.WriteLine($"[RhinoMCP] config.json ignored: {ex.Message}");
                }
                var reusable = localConfig == null ? FindReusableEndpoint() : null;
                _token = localConfig?.Token ?? reusable?.Info.Token ?? NewToken();
                StartListener(localConfig?.Port ?? reusable?.Info.Port ?? 0);
                var configuredPort = localConfig?.Port ?? reusable?.Info.Port ?? 0;
                if (configuredPort != 0 && configuredPort != _port)
                {
                    // The saved/configured port was occupied by another process.
                    // Do not reuse the old credential for a different listener.
                    _token = NewToken();
                    RhinoApp.WriteLine($"[RhinoMCP] configured port {configuredPort} is unavailable; using port {_port} with a new token.");
                }
                WriteEndpointFile();
                // Retain a stable config across clean shutdowns. Never replace
                // another instance's existing configuration after a port conflict.
                EndpointConfiguration.SaveIfMissing(profile, _port, _token);
                DeleteEndpointFile(_replacedEndpointFile);
                _replacedEndpointFile = null;
                InstallCrashGuards();

                _listenerTask = Task.Run(() => ListenerSupervisor(_cts.Token));
                foreach (var address in _listenAddresses)
                    RhinoApp.WriteLine($"[RhinoMCP] listening on {address}:{_port}");
                return LoadReturnCode.Success;
            }
            catch (Exception ex)
            {
                errorMessage = ex.Message;
                StopRuntime();
                return LoadReturnCode.ErrorShowDialog;
            }
        }

        private static string NewToken()
        {
            return Convert.ToBase64String(RandomNumberGenerator.GetBytes(32));
        }

        private void StartListener(int requestedPort)
        {
            try
            {
                StartListenerOnPort(requestedPort);
            }
            catch (SocketException) when (requestedPort != 0)
            {
                RhinoApp.WriteLine($"[RhinoMCP] saved port {requestedPort} is unavailable; falling back to a dynamic port.");
                StartListenerOnPort(0);
            }
        }

        private void StartListenerOnPort(int port)
        {
            _listener = new TcpListener(IPAddress.Any, port);
            try
            {
                _listener.Start(MaxConcurrentConnections);
            }
            catch
            {
                _listener.Stop();
                _listener = null;
                throw;
            }
            _port = ((IPEndPoint)_listener.LocalEndpoint).Port;
            _listenAddresses = GetAllowedLocalAddresses();
        }

        private void WriteEndpointFile()
        {
            var directory = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".rhino-mcp");
            Directory.CreateDirectory(directory);
            if (string.IsNullOrEmpty(_endpointFile))
            {
                _endpointFile = Path.Combine(directory,
                    $"plugin-port-{Environment.ProcessId}-{Guid.NewGuid():N}.json");
            }

            if (!OperatingSystem.IsWindows())
                new DirectoryInfo(directory).UnixFileMode =
                    UnixFileMode.UserRead | UnixFileMode.UserWrite | UnixFileMode.UserExecute;

            var json = JsonSerializer.Serialize(new EndpointInfo
            {
                Port = _port,
                Token = _token,
                ProcessId = Environment.ProcessId,
                Addresses = _listenAddresses.Select(address => address.ToString()).ToArray(),
            });
            var temporaryFile = _endpointFile + "." + Guid.NewGuid().ToString("N") + ".tmp";
            File.WriteAllText(temporaryFile, json, new UTF8Encoding(false));
            if (!OperatingSystem.IsWindows())
                File.SetUnixFileMode(temporaryFile, UnixFileMode.UserRead | UnixFileMode.UserWrite);
            File.Move(temporaryFile, _endpointFile, true);
            if (!OperatingSystem.IsWindows())
                File.SetUnixFileMode(_endpointFile, UnixFileMode.UserRead | UnixFileMode.UserWrite);
        }

        private async Task ListenerSupervisor(CancellationToken cancellationToken)
        {
            while (!cancellationToken.IsCancellationRequested)
            {
                try
                {
                    await AcceptLoop(cancellationToken);
                    if (cancellationToken.IsCancellationRequested)
                        break;

                    SafeWriteLine("[RhinoMCP] listener stopped unexpectedly; restarting.");
                }
                catch (Exception ex) when (!cancellationToken.IsCancellationRequested)
                {
                    SafeWriteLine($"[RhinoMCP] listener supervisor error: {ex.Message}; restarting.");
                }

                if (cancellationToken.IsCancellationRequested)
                    break;

                try
                {
                    RestartListener();
                    await Task.Delay(250, cancellationToken);
                }
                catch (OperationCanceledException) when (cancellationToken.IsCancellationRequested)
                {
                    break;
                }
                catch (Exception ex)
                {
                    SafeWriteLine($"[RhinoMCP] listener restart failed: {ex.Message}");
                    try { await Task.Delay(1000, cancellationToken); }
                    catch (OperationCanceledException) when (cancellationToken.IsCancellationRequested) { break; }
                }
            }
        }

        private async Task AcceptLoop(CancellationToken cancellationToken)
        {
            while (!cancellationToken.IsCancellationRequested && _listener != null)
            {
                try
                {
                    var client = await _listener.AcceptTcpClientAsync(cancellationToken);
                    _ = HandleSafely(client, cancellationToken);
                }
                catch (OperationCanceledException) when (cancellationToken.IsCancellationRequested)
                {
                    break;
                }
                catch (SocketException) when (cancellationToken.IsCancellationRequested)
                {
                    break;
                }
                catch (ObjectDisposedException)
                {
                    break;
                }
                catch (Exception ex)
                {
                    SafeWriteLine($"[RhinoMCP] listener error: {ex.Message}");
                }
            }
        }

        private async Task HandleSafely(TcpClient client, CancellationToken cancellationToken)
        {
            try
            {
                await Handle(client, cancellationToken);
            }
            catch (Exception ex)
            {
                // A malformed request or an unexpected operation failure must not
                // become an unobserved task exception or stop the listener.
                SafeWriteLine($"[RhinoMCP] request handler error: {ex.Message}");
                try { client.Dispose(); } catch { }
            }
        }

        private void RestartListener()
        {
            lock (_lifecycleGate)
            {
                if (Volatile.Read(ref _shutdownStarted) != 0 || _cts?.IsCancellationRequested == true)
                    return;

                _listener?.Stop();
                _listener = null;

                try
                {
                    StartListenerOnPort(_port);
                }
                catch (SocketException)
                {
                    StartListenerOnPort(0);
                    _token = NewToken();
                }

                try
                {
                    WriteEndpointFile();
                }
                catch
                {
                    _listener?.Stop();
                    _listener = null;
                    throw;
                }
            }
        }

        private ReusableEndpoint FindReusableEndpoint()
        {
            var directory = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".rhino-mcp");
            if (!Directory.Exists(directory))
                return null;

            var currentProcessId = Environment.ProcessId;
            var candidates = new List<ReusableEndpoint>();
            try
            {
                foreach (var file in Directory.EnumerateFiles(directory, "plugin-port-*.json")
                    .OrderByDescending(GetLastWriteTimeUtc))
                {
                    try
                    {
                        var info = JsonSerializer.Deserialize<EndpointInfo>(File.ReadAllText(file));
                        if (info == null || info.Port < 1 || info.Port > 65535 ||
                            string.IsNullOrWhiteSpace(info.Token) || info.Token.Length < 32 ||
                            info.ProcessId < 1)
                            continue;

                        if (info.ProcessId != currentProcessId && IsProcessRunning(info.ProcessId))
                            continue;

                        candidates.Add(new ReusableEndpoint(file, info));
                    }
                    catch (Exception ex) when (ex is IOException || ex is UnauthorizedAccessException ||
                                               ex is JsonException || ex is NotSupportedException)
                    {
                        // Ignore files being replaced, inaccessible files and stale JSON.
                    }
                }
            }
            catch (Exception ex) when (ex is IOException || ex is UnauthorizedAccessException)
            {
                RhinoApp.WriteLine($"[RhinoMCP] endpoint discovery warning: {ex.Message}");
            }

            var selected = candidates.FirstOrDefault();
            if (selected != null)
            {
                _replacedEndpointFile = selected.Path;
                RhinoApp.WriteLine($"[RhinoMCP] reusing local endpoint configuration from {selected.Path}");
            }

            foreach (var candidate in candidates.Skip(selected == null ? 0 : 1))
                DeleteEndpointFile(candidate.Path);

            return selected;
        }

        private static DateTime GetLastWriteTimeUtc(string path)
        {
            try { return File.GetLastWriteTimeUtc(path); }
            catch { return DateTime.MinValue; }
        }

        private static bool IsProcessRunning(int processId)
        {
            try
            {
                using (var process = Process.GetProcessById(processId))
                    return !process.HasExited;
            }
            catch (ArgumentException)
            {
                return false;
            }
            catch (InvalidOperationException)
            {
                return false;
            }
            catch (System.ComponentModel.Win32Exception)
            {
                // Access-denied is treated as live so another Rhino instance's
                // endpoint is never deleted just because it cannot be inspected.
                return true;
            }
        }

        private void InstallCrashGuards()
        {
            _unhandledExceptionHandler = (_, args) =>
            {
                SafeWriteLine($"[RhinoMCP] unhandled exception: {args.ExceptionObject}");
                CleanupEndpointFiles();
            };
            _unobservedTaskExceptionHandler = (_, args) =>
            {
                args.SetObserved();
                SafeWriteLine($"[RhinoMCP] unobserved task exception: {args.Exception}");
                CleanupEndpointFiles();
            };
            AppDomain.CurrentDomain.UnhandledException += _unhandledExceptionHandler;
            TaskScheduler.UnobservedTaskException += _unobservedTaskExceptionHandler;
        }

        private void UninstallCrashGuards()
        {
            if (_unhandledExceptionHandler != null)
                AppDomain.CurrentDomain.UnhandledException -= _unhandledExceptionHandler;
            if (_unobservedTaskExceptionHandler != null)
                TaskScheduler.UnobservedTaskException -= _unobservedTaskExceptionHandler;
            _unhandledExceptionHandler = null;
            _unobservedTaskExceptionHandler = null;
        }

        private void CleanupEndpointFiles()
        {
            DeleteEndpointFile(_endpointFile);
            DeleteEndpointFile(_replacedEndpointFile);
        }

        private static void DeleteEndpointFile(string path)
        {
            if (string.IsNullOrEmpty(path))
                return;

            try { File.Delete(path); } catch { }
        }

        private static void SafeWriteLine(string message)
        {
            try { RhinoApp.WriteLine(message); } catch { }
        }

        private async Task Handle(TcpClient client, CancellationToken cancellationToken)
        {
            using (client)
            {
                var stream = client.GetStream();
                if (!_connectionGate.Wait(0))
                {
                    await TryWriteJson(stream, 503,
                        new { success = false, error = "Too many concurrent connections." });
                    return;
                }

                try
                {
                    var remoteEndPoint = client.Client.RemoteEndPoint as IPEndPoint;
                    if (remoteEndPoint == null || !IsAllowedRemoteAddress(remoteEndPoint.Address))
                    {
                        await WriteJson(stream, 403, new { success = false, error = "Forbidden" }, cancellationToken);
                        return;
                    }

                    HttpRequestData request;
                    using (var readTimeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken))
                    {
                        readTimeout.CancelAfter(TimeSpan.FromSeconds(15));
                        try
                        {
                            request = await new HttpRequestReader(stream).ReadAsync(readTimeout.Token);
                        }
                        catch (HttpProtocolException ex)
                        {
                            await WriteJson(stream, ex.StatusCode,
                                new { success = false, error = ex.Message }, cancellationToken);
                            return;
                        }
                        catch (OperationCanceledException) when (!cancellationToken.IsCancellationRequested)
                        {
                            await WriteJson(stream, 408,
                                new { success = false, error = "Request read timed out." }, cancellationToken);
                            return;
                        }
                    }

                    if (!IsAuthorized(request.Authorization))
                    {
                        await WriteJson(stream, 401, new { success = false, error = "Unauthorized" }, cancellationToken);
                        return;
                    }

                    if (request.Path == "/health" && request.Method == "GET")
                    {
                        await WriteJson(stream, 200, new { success = true }, cancellationToken);
                        return;
                    }
                    if (request.Path != "/mcp")
                    {
                        await WriteJson(stream, 404, new { success = false, error = "Not found" }, cancellationToken);
                        return;
                    }
                    if (request.Method != "POST")
                    {
                        await WriteJson(stream, 405, new { success = false, error = "POST required" }, cancellationToken);
                        return;
                    }

                    Req payload;
                    try
                    {
                        payload = JsonSerializer.Deserialize<Req>(request.Body);
                    }
                    catch (JsonException ex)
                    {
                        await WriteJson(stream, 400, new { success = false, error = $"Invalid JSON: {ex.Message}" }, cancellationToken);
                        return;
                    }

                    if (payload == null || string.IsNullOrWhiteSpace(payload.Action) || payload.Params == null)
                    {
                        await WriteJson(stream, 400,
                            new { success = false, error = "Request must contain action and params." }, cancellationToken);
                        return;
                    }

                    await _operationGate.WaitAsync(cancellationToken);
                    try
                    {
                        var result = await Execute(payload);
                        await WriteJson(stream, 200, new { success = true, data = result }, cancellationToken);
                    }
                    finally
                    {
                        _operationGate.Release();
                    }
                }
                catch (OperationCanceledException)
                {
                    await TryWriteJson(stream, 503,
                        new { success = false, error = "Rhino is shutting down." });
                }
                catch (Exception ex)
                {
                    await TryWriteJson(stream, 500, new { success = false, error = ex.Message });
                }
                finally
                {
                    _connectionGate.Release();
                }
            }
        }

        private async Task<string> Execute(Req request)
        {
            switch (request.Action)
            {
                case "run_command":
                    return await Executor.RunAsync(GetString(request.Params, "command"));

                case "run_script":
                    return await Executor.RunScriptAsync(
                        GetString(request.Params, "code"),
                        GetString(request.Params, "lang", "python"));

                case "capture":
                    var width = GetInteger(request.Params, "width", 1920);
                    var height = GetInteger(request.Params, "height", 1080);
                    if ((long)width * height > MaxCapturePixels)
                        throw new ArgumentOutOfRangeException("width", $"Capture is limited to {MaxCapturePixels} pixels.");
                    return await Executor.InvokeOnUiThreadAsync(() => CaptureTool.Go(width, height));

                default:
                    throw new ArgumentException($"Unknown action: {request.Action}");
            }
        }

        private bool IsAuthorized(string authorization)
        {
            const string prefix = "Bearer ";
            if (string.IsNullOrEmpty(authorization) || !authorization.StartsWith(prefix, StringComparison.Ordinal))
                return false;

            var supplied = Encoding.UTF8.GetBytes(authorization.Substring(prefix.Length));
            var expected = Encoding.UTF8.GetBytes(_token ?? string.Empty);
            return supplied.Length == expected.Length && CryptographicOperations.FixedTimeEquals(supplied, expected);
        }

        private static string GetString(System.Collections.Generic.Dictionary<string, JsonElement> parameters,
            string name, string defaultValue = null)
        {
            if (!parameters.TryGetValue(name, out var value) || value.ValueKind == JsonValueKind.Null)
            {
                if (defaultValue != null)
                    return defaultValue;
                throw new ArgumentException($"Missing string parameter: {name}");
            }
            if (value.ValueKind != JsonValueKind.String)
                throw new ArgumentException($"Parameter '{name}' must be a string.");
            return value.GetString();
        }

        private static int GetInteger(System.Collections.Generic.Dictionary<string, JsonElement> parameters,
            string name, int defaultValue)
        {
            if (!parameters.TryGetValue(name, out var value) || value.ValueKind == JsonValueKind.Null)
                return defaultValue;
            if (value.ValueKind != JsonValueKind.Number || !value.TryGetInt32(out var result))
                throw new ArgumentException($"Parameter '{name}' must be an integer.");
            return result;
        }

        private static bool IsAllowedRemoteAddress(IPAddress address)
        {
            if (address == null)
                return false;
            if (address.IsIPv4MappedToIPv6)
                address = address.MapToIPv4();

            var bytes = address.GetAddressBytes();
            if (bytes.Length != 4)
                return false;

            return address.Equals(IPAddress.Loopback) ||
                (bytes[0] == 192 && bytes[1] == 168) ||
                bytes[0] == 10;
        }

        private static IPAddress[] GetAllowedLocalAddresses()
        {
            var addresses = new HashSet<IPAddress> { IPAddress.Loopback };
            foreach (var network in NetworkInterface.GetAllNetworkInterfaces())
            {
                if (network.OperationalStatus != OperationalStatus.Up)
                    continue;

                try
                {
                    foreach (var unicast in network.GetIPProperties().UnicastAddresses)
                    {
                        var address = unicast.Address;
                        if (address.AddressFamily == AddressFamily.InterNetwork && IsAllowedRemoteAddress(address))
                            addresses.Add(address);
                    }
                }
                catch (NetworkInformationException)
                {
                    // Ignore network adapters that disappear while Rhino is starting.
                }
            }

            return addresses
                .OrderBy(address => IPAddress.Loopback.Equals(address) ? 0 : 1)
                .ThenBy(address => address.ToString(), StringComparer.Ordinal)
                .ToArray();
        }

        private static async Task WriteJson(Stream stream, int statusCode, object payload,
            CancellationToken cancellationToken)
        {
            var bytes = Encoding.UTF8.GetBytes(JsonSerializer.Serialize(payload));
            var header = Encoding.ASCII.GetBytes(
                $"HTTP/1.1 {statusCode} {GetReasonPhrase(statusCode)}\r\n" +
                "Content-Type: application/json; charset=utf-8\r\n" +
                $"Content-Length: {bytes.Length}\r\n" +
                "Connection: close\r\n" +
                "Cache-Control: no-store\r\n\r\n");
            await stream.WriteAsync(header, 0, header.Length, cancellationToken);
            await stream.WriteAsync(bytes, 0, bytes.Length, cancellationToken);
            await stream.FlushAsync(cancellationToken);
        }

        private static string GetReasonPhrase(int statusCode)
        {
            switch (statusCode)
            {
                case 200: return "OK";
                case 400: return "Bad Request";
                case 401: return "Unauthorized";
                case 403: return "Forbidden";
                case 404: return "Not Found";
                case 405: return "Method Not Allowed";
                case 408: return "Request Timeout";
                case 411: return "Length Required";
                case 413: return "Payload Too Large";
                case 417: return "Expectation Failed";
                case 431: return "Request Header Fields Too Large";
                case 500: return "Internal Server Error";
                case 501: return "Not Implemented";
                case 503: return "Service Unavailable";
                default: return "Error";
            }
        }

        private static async Task TryWriteJson(Stream stream, int statusCode, object payload)
        {
            try { await WriteJson(stream, statusCode, payload, CancellationToken.None); } catch { }
        }

        private sealed class HttpRequestReader
        {
            private const int MaxHeaderBytes = 16 * 1024;
            private readonly BufferedStream _stream;
            private readonly byte[] _singleByte = new byte[1];
            private int _headerBytes;

            public HttpRequestReader(Stream stream)
            {
                _stream = new BufferedStream(stream, 8192);
            }

            public async Task<HttpRequestData> ReadAsync(CancellationToken cancellationToken)
            {
                var requestLine = await ReadHeaderLineAsync(cancellationToken);
                var requestParts = requestLine.Split(new[] { ' ' }, StringSplitOptions.RemoveEmptyEntries);
                if (requestParts.Length != 3 ||
                    (requestParts[2] != "HTTP/1.0" && requestParts[2] != "HTTP/1.1") ||
                    !requestParts[1].StartsWith("/", StringComparison.Ordinal))
                    throw new HttpProtocolException(400, "Invalid HTTP request line.");

                var headers = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
                while (true)
                {
                    var line = await ReadHeaderLineAsync(cancellationToken);
                    if (line.Length == 0)
                        break;

                    if (line[0] == ' ' || line[0] == '\t')
                        throw new HttpProtocolException(400, "Folded HTTP headers are not supported.");

                    var colon = line.IndexOf(':');
                    if (colon <= 0)
                        throw new HttpProtocolException(400, "Invalid HTTP header.");

                    var name = line.Substring(0, colon).Trim();
                    var value = line.Substring(colon + 1).Trim();
                    if (!headers.TryAdd(name, value))
                        throw new HttpProtocolException(400, $"Duplicate HTTP header: {name}.");
                }

                if (headers.ContainsKey("Transfer-Encoding"))
                    throw new HttpProtocolException(501, "Chunked request bodies are not supported.");
                if (headers.ContainsKey("Expect"))
                    throw new HttpProtocolException(417, "HTTP expectations are not supported.");

                var contentLength = 0;
                if (headers.TryGetValue("Content-Length", out var contentLengthText))
                {
                    if (!int.TryParse(contentLengthText, NumberStyles.None, CultureInfo.InvariantCulture, out contentLength) || contentLength < 0)
                        throw new HttpProtocolException(400, "Invalid Content-Length header.");
                }
                else if (requestParts[0] == "POST")
                {
                    throw new HttpProtocolException(411, "Content-Length is required.");
                }

                if (contentLength > MaxRequestBodyBytes)
                    throw new HttpProtocolException(413, $"Request body exceeds {MaxRequestBodyBytes} bytes.");

                var bodyBytes = new byte[contentLength];
                var bodyOffset = 0;
                while (bodyOffset < bodyBytes.Length)
                {
                    var read = await _stream.ReadAsync(bodyBytes, bodyOffset, bodyBytes.Length - bodyOffset, cancellationToken);
                    if (read == 0)
                        throw new HttpProtocolException(400, "Request body ended before Content-Length bytes were received.");
                    bodyOffset += read;
                }

                string body;
                try
                {
                    body = new UTF8Encoding(false, true).GetString(bodyBytes);
                }
                catch (DecoderFallbackException)
                {
                    throw new HttpProtocolException(400, "Request body must be valid UTF-8.");
                }

                var path = requestParts[1].Split('?')[0];
                headers.TryGetValue("Authorization", out var authorization);
                return new HttpRequestData(requestParts[0], path, authorization, body);
            }

            private async Task<string> ReadHeaderLineAsync(CancellationToken cancellationToken)
            {
                using var line = new MemoryStream();
                while (true)
                {
                    var read = await _stream.ReadAsync(_singleByte, 0, 1, cancellationToken);
                    if (read == 0)
                        throw new HttpProtocolException(400, "Connection closed before HTTP headers were complete.");

                    _headerBytes++;
                    if (_headerBytes > MaxHeaderBytes)
                        throw new HttpProtocolException(431, "HTTP headers are too large.");

                    var value = _singleByte[0];
                    if (value == (byte)'\n')
                    {
                        var bytes = line.ToArray();
                        if (bytes.Length == 0 || bytes[bytes.Length - 1] != (byte)'\r')
                            throw new HttpProtocolException(400, "HTTP lines must end with CRLF.");

                        for (var index = 0; index < bytes.Length - 1; index++)
                        {
                            var headerByte = bytes[index];
                            if (headerByte > 0x7f || headerByte == (byte)'\r' ||
                                (headerByte < 0x20 && headerByte != (byte)'\t'))
                                throw new HttpProtocolException(400, "HTTP headers contain invalid characters.");
                        }

                        return Encoding.ASCII.GetString(bytes, 0, bytes.Length - 1);
                    }

                    line.WriteByte(value);
                }
            }
        }

        private sealed class HttpRequestData
        {
            public HttpRequestData(string method, string path, string authorization, string body)
            {
                Method = method;
                Path = path;
                Authorization = authorization;
                Body = body;
            }

            public string Method { get; }
            public string Path { get; }
            public string Authorization { get; }
            public string Body { get; }
        }

        private sealed class HttpProtocolException : Exception
        {
            public HttpProtocolException(int statusCode, string message) : base(message)
            {
                StatusCode = statusCode;
            }

            public int StatusCode { get; }
        }

        protected override void OnShutdown()
        {
            StopRuntime();
            base.OnShutdown();
        }

        private void StopRuntime()
        {
            if (Interlocked.Exchange(ref _shutdownStarted, 1) != 0)
                return;

            lock (_lifecycleGate)
            {
                try { _cts?.Cancel(); } catch { }
                try { _listener?.Stop(); } catch { }
                CleanupEndpointFiles();
                UninstallCrashGuards();
                try { _cts?.Dispose(); } catch { }
            }
        }

        private sealed class Req
        {
            public Req() { }

            [JsonPropertyName("action")]
            public string Action { get; set; }

            [JsonPropertyName("params")]
            public System.Collections.Generic.Dictionary<string, JsonElement> Params { get; set; }
        }

        private sealed class EndpointInfo
        {
            [JsonPropertyName("port")]
            public int Port { get; set; }

            [JsonPropertyName("token")]
            public string Token { get; set; }

            [JsonPropertyName("processId")]
            public int ProcessId { get; set; }

            [JsonPropertyName("addresses")]
            public string[] Addresses { get; set; }
        }

        private sealed class ReusableEndpoint
        {
            public ReusableEndpoint(string path, EndpointInfo info)
            {
                Path = path;
                Info = info;
            }

            public string Path { get; }
            public EndpointInfo Info { get; }
        }

    }
}
