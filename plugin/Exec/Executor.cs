using System;
using System.Threading.Tasks;
using Rhino;

namespace RhinoMcpPlugin.Exec
{
    public static class Executor
    {
        public static Task<string> RunAsync(string command)
        {
            if (string.IsNullOrWhiteSpace(command))
                throw new ArgumentException("Rhino command cannot be empty.", nameof(command));

            return InvokeOnUiThreadAsync(() =>
            {
                if (!RhinoApp.RunScript(command, false))
                    throw new InvalidOperationException("Rhino rejected the command.");
                return "OK";
            });
        }

        public static Task<string> RunScriptAsync(string code, string language)
        {
            if (string.IsNullOrWhiteSpace(code))
                throw new ArgumentException("Script code cannot be empty.", nameof(code));

            switch ((language ?? "python").Trim().ToLowerInvariant())
            {
                case "python":
                    return InvokeOnUiThreadAsync(() =>
                    {
                        var script = Rhino.Runtime.PythonScript.Create();
                        if (script == null)
                            throw new InvalidOperationException("Rhino could not create a Python script context.");

                        var doc = RhinoDoc.ActiveDoc;
                        if (doc == null)
                            throw new InvalidOperationException("Python execution requires an active Rhino document.");

                        script.SetupScriptContext(doc);
                        if (!script.ExecuteScript(code))
                            throw new InvalidOperationException("Rhino Python script execution failed.");

                        return "OK";
                    });

                case "rhinoscript":
                    return InvokeOnUiThreadAsync(() =>
                    {
                        // Rhino's macro syntax accepts inline VBScript between parentheses.
                        var command = "_-RunScript (\n" + code + "\n)";
                        if (!RhinoApp.RunScript(command, false))
                            throw new InvalidOperationException("RhinoScript execution failed.");
                        return "OK";
                    });

                default:
                    throw new ArgumentException($"Unsupported script language: {language}", nameof(language));
            }
        }

        public static Task<T> InvokeOnUiThreadAsync<T>(Func<T> action)
        {
            var completion = new TaskCompletionSource<T>(TaskCreationOptions.RunContinuationsAsynchronously);
            RhinoApp.InvokeOnUiThread((Action)(() =>
            {
                try
                {
                    completion.TrySetResult(action());
                }
                catch (Exception ex)
                {
                    completion.TrySetException(ex);
                }
            }));
            return completion.Task;
        }
    }
}

