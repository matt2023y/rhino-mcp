using System;
using System.IO;
using System.Text;
using System.Threading.Tasks;
using RhinoMcpPlugin;

static class Program
{
    static void Check(bool condition, string message)
    {
        if (!condition) throw new Exception(message);
    }
    static int Main()
    {
        var root = Path.Combine(Path.GetTempPath(), "rhino-config-test-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(root);
        try
        {
            Check(EndpointConfiguration.LoadSaved(root) == null, "Empty profile must have no config");
            var token = new string('a', 44);
            Check(EndpointConfiguration.SaveIfMissing(root, 54321, token), "Initial save");
            var saved = EndpointConfiguration.LoadSaved(root);
            Check(saved.Port == 54321 && saved.Token == token, "Reuse saved settings");
            Parallel.For(0, 10, i => EndpointConfiguration.SaveIfMissing(root, 55000 + i, new string('b', 44)));
            Check(EndpointConfiguration.LoadSaved(root).Token == token, "Existing settings overwritten");
            var fresh = Path.Combine(root, "parallel");
            Parallel.For(0, 10, i => EndpointConfiguration.SaveIfMissing(fresh, 54000 + i, new string('c', 44)));
            Check(EndpointConfiguration.LoadSaved(fresh).Port >= 54000, "Concurrent publish invalid");
            File.WriteAllText(EndpointConfiguration.ConfigPath(root), "{broken");
            try { EndpointConfiguration.LoadSaved(root); throw new Exception("Accepted broken JSON"); }
            catch (InvalidDataException) { }
            Console.WriteLine("PASS: persistence, reuse, no overwrite, concurrency and invalid configuration");
            return 0;
        }
        finally { Directory.Delete(root, true); }
    }
}
