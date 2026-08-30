using System;
using System.Collections.Generic;
using System.IO;
using UnityEditor;
using UnityEngine;

namespace GoGIF.Editor
{
    public static class BatchRenderer
    {
        [Serializable]
        private sealed class Manifest
        {
            public int version;
            public int width;
            public int height;
            public int frames;
            public int delay_ms;
            public long seed;
            public string motion;
            public Paths paths;
        }

        [Serializable]
        private sealed class Paths
        {
            public string unity_frames;
            public string unity_motion;
        }

        [Serializable]
        private sealed class MotionContract
        {
            public int version;
            public List<MotionFrame> frames = new List<MotionFrame>();
        }

        [Serializable]
        private sealed class MotionFrame
        {
            public int frame;
            public float progress;
            public float yaw;
            public float pitch;
            public float camera_x;
            public float camera_y;
            public float camera_zoom;
        }

        public static void Render()
        {
            string manifestPath = Argument("-gogifManifest");
            if (String.IsNullOrWhiteSpace(manifestPath) || !File.Exists(manifestPath))
                throw new InvalidOperationException("GoGIF manifest is missing.");

            Manifest manifest = JsonUtility.FromJson<Manifest>(File.ReadAllText(manifestPath));
            Validate(manifest);
            Directory.CreateDirectory(manifest.paths.unity_frames);
            Directory.CreateDirectory(Path.GetDirectoryName(manifest.paths.unity_motion));

            GameObject root = new GameObject("GoGIF_Unity_VFX");
            GameObject cameraObject = new GameObject("GoGIF_VFX_Camera");
            Camera camera = cameraObject.AddComponent<Camera>();
            camera.transform.position = new Vector3(0f, 0f, -10f);
            camera.orthographic = true;
            camera.orthographicSize = 5f;
            camera.clearFlags = CameraClearFlags.SolidColor;
            camera.backgroundColor = new Color(0f, 0f, 0f, 0f);

            RenderTexture target = new RenderTexture(manifest.width, manifest.height, 24, RenderTextureFormat.ARGB32);
            camera.targetTexture = target;
            Texture2D pixels = new Texture2D(manifest.width, manifest.height, TextureFormat.RGBA32, false);
            List<GameObject> particles = BuildParticles(root.transform, manifest.seed);
            MotionContract motion = new MotionContract { version = manifest.version };

            try
            {
                for (int frame = 0; frame < manifest.frames; frame++)
                {
                    float progress = (float)frame / manifest.frames;
                    float phase = progress * Mathf.PI * 2f;
                    MotionFrame pose = Pose(manifest.motion, frame, progress, phase);
                    motion.frames.Add(pose);
                    AnimateParticles(particles, phase, manifest.seed);
                    camera.transform.position = new Vector3(pose.camera_x, pose.camera_y, -10f);
                    camera.orthographicSize = 5f / pose.camera_zoom;

                    camera.Render();
                    RenderTexture previous = RenderTexture.active;
                    RenderTexture.active = target;
                    pixels.ReadPixels(new Rect(0, 0, manifest.width, manifest.height), 0, 0, false);
                    pixels.Apply(false, false);
                    RenderTexture.active = previous;
                    string output = Path.Combine(manifest.paths.unity_frames, String.Format("frame-{0:D4}.png", frame));
                    File.WriteAllBytes(output, pixels.EncodeToPNG());
                }
                File.WriteAllText(manifest.paths.unity_motion, JsonUtility.ToJson(motion, true));
            }
            finally
            {
                camera.targetTexture = null;
                UnityEngine.Object.DestroyImmediate(pixels);
                UnityEngine.Object.DestroyImmediate(target);
                UnityEngine.Object.DestroyImmediate(cameraObject);
                UnityEngine.Object.DestroyImmediate(root);
            }
        }

        private static List<GameObject> BuildParticles(Transform parent, long seed)
        {
            System.Random random = new System.Random(unchecked((int)seed));
            Shader shader = Shader.Find("Unlit/Color");
            if (shader == null) throw new InvalidOperationException("Unity Unlit/Color shader is unavailable.");
            List<GameObject> result = new List<GameObject>();
            for (int index = 0; index < 28; index++)
            {
                GameObject particle = GameObject.CreatePrimitive(index % 2 == 0 ? PrimitiveType.Sphere : PrimitiveType.Cube);
                particle.name = "GoGIF_VFX_" + index;
                particle.transform.SetParent(parent, false);
                float size = 0.05f + (float)random.NextDouble() * 0.14f;
                particle.transform.localScale = Vector3.one * size;
                Material material = new Material(shader);
                float hue = (float)((random.NextDouble() + index * 0.0618) % 1.0);
                material.color = Color.HSVToRGB(hue, 0.72f, 1f);
                particle.GetComponent<Renderer>().sharedMaterial = material;
                result.Add(particle);
            }
            return result;
        }

        private static void AnimateParticles(List<GameObject> particles, float phase, long seed)
        {
            for (int index = 0; index < particles.Count; index++)
            {
                float offset = index * 2.39996f + (seed & 255) * 0.003f;
                float radius = 1.25f + (index % 7) * 0.43f;
                float angle = phase * (0.65f + (index % 5) * 0.08f) + offset;
                particles[index].transform.localPosition = new Vector3(
                    Mathf.Cos(angle) * radius,
                    Mathf.Sin(angle * 1.37f) * radius * 0.72f,
                    0f
                );
                particles[index].transform.localRotation = Quaternion.Euler(0f, 0f, angle * Mathf.Rad2Deg);
            }
        }

        private static MotionFrame Pose(string motion, int frame, float progress, float phase)
        {
            MotionFrame value = new MotionFrame { frame = frame, progress = progress, camera_zoom = 1f };
            switch (motion)
            {
                case "pulse":
                    value.camera_zoom = 1.02f + 0.07f * (0.5f + 0.5f * Mathf.Sin(phase));
                    break;
                case "waves":
                    value.camera_x = Mathf.Sin(phase) * 0.4f;
                    value.camera_y = Mathf.Sin(phase * 2f) * 0.14f;
                    value.yaw = Mathf.Sin(phase) * 8f;
                    break;
                case "confetti":
                    value.camera_x = Mathf.Sin(phase) * 0.1f;
                    value.pitch = Mathf.Sin(phase * 2f) * 4f;
                    break;
                default:
                    value.camera_x = Mathf.Cos(phase) * 0.18f;
                    value.camera_y = Mathf.Sin(phase) * 0.18f;
                    value.yaw = progress * 360f;
                    break;
            }
            return value;
        }

        private static void Validate(Manifest manifest)
        {
            if (manifest == null || manifest.version != 1 || manifest.paths == null ||
                manifest.width < 128 || manifest.width > 1024 || manifest.height < 128 || manifest.height > 1024 ||
                manifest.frames < 4 || manifest.frames > 48 || manifest.delay_ms < 20 || manifest.delay_ms > 1000)
                throw new InvalidOperationException("GoGIF manifest failed Unity bounds validation.");
        }

        private static string Argument(string name)
        {
            string[] values = Environment.GetCommandLineArgs();
            for (int index = 0; index + 1 < values.Length; index++)
                if (String.Equals(values[index], name, StringComparison.OrdinalIgnoreCase)) return values[index + 1];
            return String.Empty;
        }
    }
}
