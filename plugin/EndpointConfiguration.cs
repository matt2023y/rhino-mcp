using System;
using System.IO;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace RhinoMcpPlugin
{
    // Has no Rhino dependency so startup configuration can be tested separately.
    internal sealed class EndpointConfiguration
    {
        [JsonPropertyName("port")]
        public int Port { get; set; }
        [JsonPropertyName("token")]
        public string Token { get; set; }

        internal static string ConfigPath(string profile) =>
            Path.Combine(profile, ".rhino-mcp", "config.json");

        private static EndpointConfiguration Validate(EndpointConfiguration value)
        {
            if (value == null || value.Port < 1 || value.Port > 65535 ||
                string.IsNullOrWhiteSpace(value.Token) || value.Token.Length < 32 ||
                value.Token.IndexOfAny(new[] { '\r', '\n' }) >= 0)
                throw new InvalidDataException("Rhino MCP configuration requires port 1..65535 and a token of at least 32 characters.");
            return value;
        }

        internal static EndpointConfiguration LoadSaved(string profile)
        {
            var path = ConfigPath(profile);
            if (!File.Exists(path)) return null;
            try { return Validate(JsonSerializer.Deserialize<EndpointConfiguration>(File.ReadAllText(path))); }
            catch (JsonException) { throw new InvalidDataException("Invalid JSON in Rhino MCP config.json."); }
        }

        internal static bool SaveIfMissing(string profile, int port, string token)
        {
            var value = Validate(new EndpointConfiguration { Port = port, Token = token });
            var destination = ConfigPath(profile);
            if (File.Exists(destination)) return false;
            var directory = Path.GetDirectoryName(destination);
            Directory.CreateDirectory(directory);
            var temporary = Path.Combine(directory, "config-" + Guid.NewGuid().ToString("N") + ".tmp");
            try
            {
                File.WriteAllText(temporary, JsonSerializer.Serialize(value), new UTF8Encoding(false));
                // Atomic publish without overwrite, including concurrent first starts.
                try { File.Move(temporary, destination); }
                catch (IOException) when (File.Exists(destination)) { return false; }
                return true;
            }
            finally
            {
                if (File.Exists(temporary)) File.Delete(temporary);
            }
        }
    }
}
