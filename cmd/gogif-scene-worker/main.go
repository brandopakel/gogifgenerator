package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/cinematic"
	filmblender "github.com/brandopakel/gogifgenerator/internal/cinematic/blender"
	"github.com/brandopakel/gogifgenerator/internal/cinematic/unreal"
	"github.com/brandopakel/gogifgenerator/internal/imagegen/comfypartner"
	"github.com/brandopakel/gogifgenerator/internal/scene"
	"github.com/brandopakel/gogifgenerator/internal/sceneworker"
)

func main() {
	once := flag.Bool("once", false, "claim at most one Scene job and exit")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	fail := func(message string, err error) {
		logger.Error(message, "error", err)
		os.Exit(1)
	}

	workerID := strings.TrimSpace(os.Getenv("GOGIF_SCENE_WORKER_ID"))
	if workerID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			fail("determine Scene worker ID", err)
		}
		workerID = hostname
	}
	blenderStage, err := filmblender.New(filmblender.Options{
		Executable: envOr("GOGIF_BLENDER_EXECUTABLE", "blender"), Timeout: envDuration("GOGIF_SCENE_BLENDER_TIMEOUT", 10*time.Minute),
	})
	if err != nil {
		fail("configure Scene Blender stage", err)
	}
	unrealStage, err := unreal.New(unreal.Options{
		Executable: envOr("GOGIF_UNREAL_EXECUTABLE", "UnrealEditor-Cmd"),
		Project:    os.Getenv("GOGIF_UNREAL_PROJECT"), Script: os.Getenv("GOGIF_UNREAL_SCRIPT"),
		Timeout: envDuration("GOGIF_SCENE_UNREAL_TIMEOUT", 45*time.Minute),
	})
	if err != nil {
		fail("configure Scene Unreal stage", err)
	}
	referenceGenerator, err := comfypartner.New(comfypartner.Options{
		Endpoint: envOr("GOGIF_COMFYUI_IMAGE_URL", "https://cloud.comfy.org/api"), APIKey: os.Getenv("COMFY_CLOUD_API_KEY"),
		Recipe: envOr("GOGIF_COMFYUI_IMAGE_RECIPE", "flux-ultra"), MaxWait: envDuration("GOGIF_SCENE_REFERENCE_TIMEOUT", 10*time.Minute),
	})
	if err != nil {
		fail("configure Scene semantic reference generator", err)
	}
	ffmpegExecutable := envOr("GOGIF_FFMPEG_EXECUTABLE", "ffmpeg")
	renderer, err := sceneworker.NewUnrealRenderer(sceneworker.UnrealRendererOptions{
		Blender: blenderStage, Unreal: unrealStage, ReferenceGenerator: referenceGenerator, FFmpegExecutable: ffmpegExecutable,
		FFmpegTimeout: envDuration("GOGIF_SCENE_FFMPEG_TIMEOUT", 20*time.Minute),
	})
	if err != nil {
		fail("configure Scene renderer", err)
	}

	hello := scene.WorkerHello{
		ProtocolVersion: scene.WorkerProtocolVersion, WorkerID: workerID, WorkerVersion: sceneworker.Version,
		Targets: []scene.EngineTarget{scene.TargetUnreal},
		Capabilities: []scene.WorkerCapability{
			capability(blenderStage.Descriptor()), capability(unrealStage.Descriptor()),
			{ID: "ffmpeg", Version: commandVersion(ffmpegExecutable, "-version")},
			{ID: referenceGenerator.Descriptor().ID, Version: "hosted"},
		},
	}
	client, err := sceneworker.NewClient(sceneworker.ClientOptions{
		BaseURL: os.Getenv("GOGIF_SCENE_API_URL"), Token: os.Getenv("GOGIF_SCENE_WORKER_TOKEN"), Hello: hello,
		HTTPClient: workerHTTPClient(),
	})
	if err != nil {
		fail("configure Scene control plane", err)
	}
	worker, err := sceneworker.New(sceneworker.Options{
		API: client, Renderers: map[scene.EngineTarget]sceneworker.Renderer{scene.TargetUnreal: renderer}, Logger: logger,
		WorkspaceRoot: os.Getenv("GOGIF_SCENE_WORKSPACE_ROOT"), PollInterval: envDuration("GOGIF_SCENE_POLL_INTERVAL", 5*time.Second),
		HeartbeatInterval: envDuration("GOGIF_SCENE_HEARTBEAT_INTERVAL", 30*time.Second),
	})
	if err != nil {
		fail("configure Scene worker", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	logger.Info("GoGIF Scene worker ready", "worker", workerID, "version", sceneworker.Version, "target", scene.TargetUnreal)
	if *once {
		worked, err := worker.RunOnce(ctx)
		if err != nil {
			fail("Scene worker job failed", err)
		}
		if !worked {
			logger.Info("No compatible Scene job is queued")
		}
		return
	}
	if err := worker.Run(ctx); err != nil && ctx.Err() == nil {
		fail("Scene worker stopped", err)
	}
}

func capability(descriptor cinematic.StageDescriptor) scene.WorkerCapability {
	return scene.WorkerCapability{ID: descriptor.ID, Version: descriptor.Version}
}

func workerHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.ResponseHeaderTimeout = 2 * time.Minute
	transport.IdleConnTimeout = 90 * time.Second
	return &http.Client{Transport: transport}
}

func commandVersion(executable, argument string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, executable, argument).Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	if len(line) > 160 {
		line = line[:160]
	}
	return line
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
