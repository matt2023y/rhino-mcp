using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO;
using Rhino;
using Rhino.Display;

namespace RhinoMcpPlugin.Capture
{
    public static class CaptureTool
    {
        private const int MaxDimension = 4096;
        private const long MaxPixels = 16_000_000;

        public static string Go(int width, int height)
        {
            if (width < 1 || height < 1 || width > MaxDimension || height > MaxDimension ||
                (long)width * height > MaxPixels)
            {
                throw new ArgumentOutOfRangeException(nameof(width),
                    $"Capture dimensions must be positive, at most {MaxDimension} per side, and at most {MaxPixels} pixels total.");
            }

            var view = RhinoDoc.ActiveDoc?.Views.ActiveView;
            if (view == null)
                throw new InvalidOperationException("No active Rhino viewport is available.");

            var settings = new ViewCaptureSettings(view, new Size(width, height), 150)
            {
                RasterMode = true,
                DrawGrid = false,
                DrawAxis = false,
            };

            using var bitmap = ViewCapture.CaptureToBitmap(settings);
            if (bitmap == null)
                throw new InvalidOperationException("Rhino could not capture the active viewport.");

            using var stream = new MemoryStream();
            bitmap.Save(stream, ImageFormat.Png);
            return Convert.ToBase64String(stream.ToArray());
        }
    }
}

