package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/brandopakel/gogifgenerator/internal/auth"
	"github.com/brandopakel/gogifgenerator/internal/cinematic"
	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/modelgen"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/reference"
	"github.com/brandopakel/gogifgenerator/internal/store"
	"github.com/brandopakel/gogifgenerator/internal/video"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestPublicConfigDisablesTypedNilVideoDecoder(t *testing.T) {
	var decoder *recordingVideoDecoder
	request := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, VideoDecoder: decoder}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var publicConfig struct {
		VideoEditor struct {
			Enabled bool `json:"enabled"`
		} `json:"video_editor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&publicConfig); err != nil {
		t.Fatal(err)
	}
	if publicConfig.VideoEditor.Enabled {
		t.Fatal("typed nil video decoder was reported as enabled")
	}
}

func TestSecurityPolicyAllowsConfiguredCatalogMedia(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	policy := response.Header().Get("Content-Security-Policy")
	for _, host := range []string{
		"https://upload.wikimedia.org", "https://blob.gifcities.org", "https://archive.org", "https://*.archive.org", "https://images-assets.nasa.gov",
		"https://y.yarn.co", "frame-src https://getyarn.io",
	} {
		if !strings.Contains(policy, host) {
			t.Fatalf("Content-Security-Policy does not allow %s: %q", host, policy)
		}
	}
}

func TestGenerate(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "tests are green",
      "width": 128,
      "height": 128,
      "frames": 4
    }`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "image/gif" {
		t.Fatalf("Content-Type = %q", got)
	}
	if _, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("DecodeAll() error = %v", err)
	}
}

func TestLocalAccountCreationIsOwnedListedAndShareable(t *testing.T) {
	kv := store.NewMemoryKV()
	mediaRepository := media.NewRepository(kv)
	blobs, err := store.NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	library := media.NewLibrary(mediaRepository, blobs)
	accounts := account.NewRepository(kv)
	plans := account.NewCatalog(account.CatalogOptions{})
	authManager, err := auth.New(auth.Options{
		Mode: auth.ModeLocal, SessionSecret: strings.Repeat("s", 32), PublicURL: "http://example.com", LocalEmail: "owner@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		Planner: planner.Local{}, Auth: authManager, Accounts: accounts, Plans: plans, Usage: account.NewLedger(kv),
		LibraryCatalog: mediaRepository, GeneratedSaver: library, GeneratedReader: library,
	})

	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt":"saved for later","width":128,"height":128,"frames":4,"generation_mode":"fast"
    }`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("generate status = %d; body = %s", response.Code, response.Body.String())
	}
	assetID := response.Header().Get("X-GoGIF-Asset-ID")
	if assetID == "" {
		t.Fatal("generated response omitted asset ID")
	}
	asset, err := mediaRepository.Get(context.Background(), assetID)
	if err != nil || asset.OwnerID != "usr_local" {
		t.Fatalf("saved asset = %#v, %v", asset, err)
	}

	request = httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/library", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	blobKey := ""
	if len(asset.Renditions) > 0 {
		blobKey = asset.Renditions[0].BlobKey
	}
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), assetID) || (blobKey != "" && strings.Contains(response.Body.String(), blobKey)) {
		t.Fatalf("library status = %d; body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/library/"+assetID+"/share", bytes.NewBufferString(`{"hours":1}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("share status = %d; body = %s", response.Code, response.Body.String())
	}
	var share struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&share); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "http://example.com"+share.URL, nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("shared asset status = %d; headers = %#v", response.Code, response.Header())
	}

	request = httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/account", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"library_usage":{"bytes":`) || !strings.Contains(response.Body.String(), `"items":1`) {
		t.Fatalf("local account status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestGuestCannotSpendOnSemanticGeneration(t *testing.T) {
	kv := store.NewMemoryKV()
	authManager, err := auth.New(auth.Options{
		Mode: auth.ModeOIDC, SessionSecret: strings.Repeat("s", 32), PublicURL: "https://example.com",
		Repository: account.NewRepository(kv), Provider: inertIdentityProvider{},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://example.com/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt":"expensive scene","width":320,"height":320,"frames":8,"generation_mode":"semantic"
    }`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, Auth: authManager, Plans: account.NewCatalog(account.CatalogOptions{}), Usage: account.NewLedger(kv)}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"sign_in_required"`) {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestAuthenticatedGIFRequiresPrivateLibraryPersistence(t *testing.T) {
	kv := store.NewMemoryKV()
	authManager, err := auth.New(auth.Options{
		Mode: auth.ModeOIDC, SessionSecret: strings.Repeat("s", 32), PublicURL: "https://example.com",
		Repository: account.NewRepository(kv), Provider: inertIdentityProvider{},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://example.com/api/v1/gifs/generate", nil)
	request = request.WithContext(account.WithPrincipal(request.Context(), account.Principal{
		ID: "usr_1", UserID: "usr_1", Email: "person@example.com", PlanID: account.PlanFree, Authenticated: true,
	}))
	response := httptest.NewRecorder()
	app := server{options: Options{Auth: authManager, Plans: account.NewCatalog(account.CatalogOptions{})}}
	saved := app.writeGenerated(response, request, planner.Request{Prompt: "private"}, planner.Result{}, []byte("GIF89a"), "test", nil)
	if saved || response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "No credits were used") {
		t.Fatalf("saved = %v; status = %d; body = %s", saved, response.Code, response.Body.String())
	}
}

func TestGenerateModelReturnsAndPersistsGLB(t *testing.T) {
	glb := append([]byte("glTF"), make([]byte, 16)...)
	generator := &recordingModelGenerator{result: modelgen.Result{
		Data: glb, ContentType: "model/gltf-binary", Extension: "glb", Engine: "comfyui/tripo-3.1",
	}}
	saver := &recordingModelSaver{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/models/generate", bytes.NewBufferString(`{"prompt":"a clockwork bird","recipe":"tripo-3.1","seed":42}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, ModelGenerator: generator, ModelSaver: saver}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "model/gltf-binary" {
		t.Fatalf("status = %d; headers = %#v; body = %s", response.Code, response.Header(), response.Body.String())
	}
	if !bytes.Equal(response.Body.Bytes(), glb) || generator.request.Prompt != "a clockwork bird" || generator.request.Recipe != "tripo-3.1" {
		t.Fatalf("response/request = %q / %#v", response.Body.Bytes(), generator.request)
	}
	if saver.generated.Engine != "comfyui/tripo-3.1" || response.Header().Get("Location") != "/api/v1/models/model_saved" {
		t.Fatalf("saved = %#v; headers = %#v", saver.generated, response.Header())
	}
}

func TestGenerateModelExplainsMissingWorkflow(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/models/generate", bytes.NewBufferString(`{"prompt":"a clockwork bird"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "ComfyUI 3D workflow") {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestGeneratedModelServesOnlyModelLibraryContent(t *testing.T) {
	reader := &recordingModelReader{data: append([]byte("glTF"), make([]byte, 16)...)}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/models/model_owned", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, ModelReader: reader}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "model/gltf-binary" || reader.id != "model_owned" {
		t.Fatalf("status = %d; headers = %#v; id = %q", response.Code, response.Header(), reader.id)
	}
}

func TestSemanticGenerationNeverFallsBackToAbstractShapes(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "a hero swinging through the city",
      "width": 128,
      "height": 128,
      "frames": 4,
      "generation_mode": "semantic"
    }`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Realistic AI generation is not configured") {
		t.Fatalf("body = %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Fast local is limited to abstract motion") {
		t.Fatalf("semantic error recommends an invalid abstract fallback: %s", response.Body.String())
	}
}

func TestSemanticGenerationUsesSemanticImageGenerator(t *testing.T) {
	generator := &recordingImageGenerator{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "a hero swinging through the city",
      "width": 128,
      "height": 128,
      "frames": 4,
      "generation_mode": "semantic"
    }`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, ImageGenerator: generator}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if generator.request.Prompt != "a hero swinging through the city" {
		t.Fatalf("Generate() request = %#v", generator.request)
	}
	if got := response.Header().Get("X-GoGIF-Engine"); got != "test-local+local" {
		t.Fatalf("X-GoGIF-Engine = %q", got)
	}
}

func TestStudioCinematicGenerationKeepsPromptBelowMedia(t *testing.T) {
	renderer := &recordingCinematicRenderer{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "a hero swinging through the city",
      "width": 128,
      "height": 128,
      "frames": 4,
      "generation_mode": "studio"
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, CinematicRenderer: renderer, ImageGenerator: &recordingImageGenerator{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if renderer.request.Spec.ShowPrompt {
		t.Fatalf("semantic cinematic request rendered a caption: %#v", renderer.request.Spec)
	}
}

func TestSemanticBackendErrorExplainsLocalComfyFailure(t *testing.T) {
	generator := &recordingImageGenerator{
		descriptor: imagegen.Descriptor{ID: "comfyui-local", Label: "ComfyUI (local)", Semantic: true},
		err:        imagegen.ErrUnavailable,
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "a hero swinging through the city",
      "width": 128,
      "height": 128,
      "frames": 4,
      "generation_mode": "semantic"
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, ImageGenerator: generator}).ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "ComfyUI Desktop is not running") {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestGenerateFromUploadEditsPhoto(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("media", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(generatedTestPNG(t)); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"caption": "WE SHIPPED", "width": "128", "height": "128", "frames": "4",
		"delay_ms": "80", "motion": "pulse", "crop_x": "0.5", "crop_y": "-0.5",
		"zoom": "1.2", "caption_position": "top", "loop": "false",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate-from-upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-GoGIF-Engine"); got != "upload-photo+go" {
		t.Fatalf("X-GoGIF-Engine = %q", got)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(animation.Image) != 4 || animation.LoopCount != -1 || animation.Config.Width != 128 {
		t.Fatalf("animation = %d frames, loop %d, width %d", len(animation.Image), animation.LoopCount, animation.Config.Width)
	}
}

func TestGenerateFromUploadRejectsUnsupportedMedia(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("media", "clip.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("not an image"))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate-from-upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestGenerateFromUploadPreservesExistingAnimation(t *testing.T) {
	var source bytes.Buffer
	animation := &gif.GIF{LoopCount: 0}
	for frameNumber := range 6 {
		frame := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White})
		frame.SetColorIndex(frameNumber, frameNumber, 1)
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, 5)
	}
	if err := gif.EncodeAll(&source, animation); err != nil {
		t.Fatal(err)
	}
	frames, pixels, err := inspectGIF(source.Bytes())
	if err != nil || frames != 6 || pixels != 6*8*8 {
		t.Fatalf("inspectGIF() = %d, %d, %v", frames, pixels, err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, _ := writer.CreateFormFile("media", "animation.gif")
	_, _ = file.Write(source.Bytes())
	_ = writer.WriteField("width", "128")
	_ = writer.WriteField("height", "128")
	_ = writer.WriteField("frames", "4")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate-from-upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-GoGIF-Engine") != "upload-gif+go" {
		t.Fatalf("engine = %q", response.Header().Get("X-GoGIF-Engine"))
	}
	exported, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Image) != 4 {
		t.Fatalf("exported animation = %d frames", len(exported.Image))
	}
}

func TestGenerateFromUploadTrimsVideoWithConfiguredDecoder(t *testing.T) {
	decoder := &recordingVideoDecoder{}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, _ := writer.CreateFormFile("media", "clip.mp4")
	_, _ = file.Write(append([]byte{0, 0, 0, 20}, []byte("ftypisomvideo")...))
	for key, value := range map[string]string{
		"width": "128", "height": "128", "frames": "4", "trim_start_ms": "1000", "trim_end_ms": "2500",
	} {
		_ = writer.WriteField(key, value)
	}
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate-from-upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, VideoDecoder: decoder}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if decoder.request.StartMS != 1000 || decoder.request.EndMS != 2500 || decoder.request.Frames != 4 {
		t.Fatalf("Decode() request = %#v", decoder.request)
	}
	if response.Header().Get("X-GoGIF-Engine") != "upload-video+test-video+go" {
		t.Fatalf("engine = %q", response.Header().Get("X-GoGIF-Engine"))
	}
	if _, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("DecodeAll() error = %v", err)
	}
}

func TestGenerateFromUploadReportsUnavailableVideoEditor(t *testing.T) {
	var decoder *recordingVideoDecoder
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, _ := writer.CreateFormFile("media", "clip.mp4")
	_, _ = file.Write(append([]byte{0, 0, 0, 20}, []byte("ftypisomvideo")...))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate-from-upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, VideoDecoder: decoder}).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "FFmpeg") {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestOptimizeUploadGIFReducesDimensionsAndFramesToTarget(t *testing.T) {
	spec := gifdomain.Defaults()
	rendered, exported, err := optimizeUploadGIF(spec, 300_000, func(candidate gifdomain.Spec) ([]byte, error) {
		return make([]byte, candidate.Width*candidate.Height*candidate.Frames/8), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered) > 300_000 || exported.Width >= spec.Width || exported.Frames >= spec.Frames {
		t.Fatalf("export = %d bytes, %#v", len(rendered), exported)
	}
}

func TestGenerateAnimatesLocalImageGeneratorOutput(t *testing.T) {
	generator := &recordingImageGenerator{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "a tiny local robot",
      "width": 128,
      "height": 128,
      "frames": 4,
      "generation_mode": "semantic"
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, ImageGenerator: generator}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-GoGIF-Engine"); got != "test-local+local" {
		t.Fatalf("X-GoGIF-Engine = %q", got)
	}
	if generator.request.Prompt != "a tiny local robot" || generator.request.Width != 128 || generator.request.Height != 128 {
		t.Fatalf("Generate() request = %#v", generator.request)
	}
	if _, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("DecodeAll() error = %v", err)
	}
}

func TestGenerateUsesCinematicPipelineBeforeStillGenerator(t *testing.T) {
	renderer := &recordingCinematicRenderer{}
	generator := &recordingImageGenerator{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "cinematic robot",
      "width": 128,
      "height": 128,
      "frames": 4,
      "generation_mode": "studio"
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, CinematicRenderer: renderer, ImageGenerator: generator}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-GoGIF-Engine"); got != "blender+unity-6.3+unreal-5+ffmpeg+local" {
		t.Fatalf("X-GoGIF-Engine = %q", got)
	}
	if renderer.request.Prompt != "cinematic robot" || renderer.request.Spec.Width != 128 {
		t.Fatalf("Render() request = %#v", renderer.request)
	}
	if generator.request.Prompt != "" {
		t.Fatalf("still generator unexpectedly ran: %#v", generator.request)
	}
}

func TestFastLocalSkipsImageAndCinematicGenerators(t *testing.T) {
	renderer := &recordingCinematicRenderer{}
	generator := &recordingImageGenerator{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "do this without cloud or editors",
      "width": 128,
      "height": 128,
      "frames": 4,
      "generation_mode": "fast"
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, CinematicRenderer: renderer, ImageGenerator: generator}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if generator.request.Prompt != "" || renderer.request.Prompt != "" {
		t.Fatalf("fast mode called configured generators: image=%#v cinematic=%#v", generator.request, renderer.request)
	}
	if got := response.Header().Get("X-GoGIF-Engine"); got != "local" {
		t.Fatalf("X-GoGIF-Engine = %q", got)
	}
}

func TestStudioModeExplainsMissingLocalPipeline(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "studio robot",
      "width": 128,
      "height": 128,
      "frames": 4,
      "generation_mode": "studio"
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, ImageGenerator: &recordingImageGenerator{}}).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "Studio Local is not configured") {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestGenerateFromReferenceRevalidatesFetchesAndDeletesSource(t *testing.T) {
	localGenerator := &recordingImageGenerator{}
	saver := &recordingSaver{}
	sourcePNG := generatedTestPNG(t)
	sourceServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(sourcePNG)
	}))
	defer sourceServer.Close()
	sourceURL, _ := url.Parse(sourceServer.URL)
	temporaryDirectory := t.TempDir()
	fetcher, err := reference.New(reference.Options{
		Client: sourceServer.Client(), TempDir: temporaryDirectory,
		AllowedHosts: map[string][]string{"test": {sourceURL.Hostname()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mediaProvider := &recordingProvider{result: provider.Result{
		Provider: "test", ExternalID: "42", Kind: media.KindImage, OriginalURL: sourceServer.URL,
		Derivatives: media.PermissionAllowed, TransformPolicy: provider.TransformAllowed,
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate-from-reference", bytes.NewBufferString(`{
      "provider":"test", "external_id":"42", "prompt":"make this dance",
      "width":128, "height":128, "frames":4
    }`))
	response := httptest.NewRecorder()
	New(Options{
		Planner: planner.Local{}, Providers: []provider.Provider{mediaProvider},
		ImageGenerator: localGenerator, ReferenceFetcher: fetcher, GeneratedSaver: saver,
	}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if mediaProvider.resolvedID != "42" || len(localGenerator.request.Inputs) != 1 || localGenerator.request.Inputs[0].SourceID != "test:42" {
		t.Fatalf("resolved ID = %q; generator request = %#v", mediaProvider.resolvedID, localGenerator.request)
	}
	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary references = %#v, %v", entries, err)
	}
	if _, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("DecodeAll() error = %v", err)
	}
	if saver.generated.Source == nil || saver.generated.Source.Provider != "test" || saver.generated.Source.ExternalID != "42" {
		t.Fatalf("persisted source = %#v", saver.generated.Source)
	}
}

func TestGeneratePersistsAssetWhenConfigured(t *testing.T) {
	saver := &recordingSaver{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{
      "prompt": "save this one",
      "width": 128,
      "height": 128,
      "frames": 4
    }`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, GeneratedSaver: saver}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-GoGIF-Asset-ID"); got != "gif_saved" {
		t.Fatalf("X-GoGIF-Asset-ID = %q", got)
	}
	if got := response.Header().Get("Location"); got != "/api/v1/gifs/gif_saved" {
		t.Fatalf("Location = %q", got)
	}
	if saver.generated.Prompt != "save this one" || len(saver.generated.Data) == 0 {
		t.Fatalf("SaveGenerated() input = %#v", saver.generated)
	}
}

func TestGenerateRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/gifs/generate", bytes.NewBufferString(`{"prompt":"hi","surprise":true}`))
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestGeneratedServesOnlyConfiguredLibraryContent(t *testing.T) {
	reader := &recordingGeneratedReader{data: []byte("GIF89a-generated")}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/gifs/gif_owned", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, GeneratedReader: reader}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "GIF89a-generated" {
		t.Fatalf("status = %d; body = %q", response.Code, response.Body.String())
	}
	if reader.id != "gif_owned" || response.Header().Get("Content-Type") != "image/gif" || response.Header().Get("X-GoGIF-Engine") != "test-engine" {
		t.Fatalf("id = %q; headers = %#v", reader.id, response.Header())
	}
}

func TestSearchProvider(t *testing.T) {
	fake := &recordingProvider{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/test/search?q=victory&limit=3&cursor=12&locale=en", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, Providers: []provider.Provider{fake}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if fake.query.Text != "victory" || fake.query.Limit != 3 || fake.query.Cursor != "12" || fake.query.Locale != "en" {
		t.Fatalf("Search() query = %#v", fake.query)
	}
	var page provider.Page
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Provider != "test" || len(page.Results) != 1 || page.Results[0].ExternalID != "result-1" {
		t.Fatalf("page = %#v", page)
	}
}

func TestSearchProviderRejectsBadLimit(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/test/search?q=victory&limit=many", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, Providers: []provider.Provider{&recordingProvider{}}}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestSearchProviderReturnsNotFound(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/missing/search?q=victory", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}}).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestResolveProviderItem(t *testing.T) {
	fake := &recordingProvider{result: provider.Result{
		Provider: "test", ExternalID: "clip-1", Title: "Resolved clip", Kind: media.KindVideo,
		Renditions: []provider.Rendition{{Name: "360p", ContentType: "video/mp4", URL: "https://example.com/clip.mp4"}},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/test/items/clip-1?locale=fr", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, Providers: []provider.Provider{fake}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if fake.resolvedID != "clip-1" || fake.resolvedLocale != "fr" {
		t.Fatalf("resolved ID = %q; locale = %q", fake.resolvedID, fake.resolvedLocale)
	}
	var result provider.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != media.KindVideo || len(result.Renditions) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestResolveProviderItemQuote(t *testing.T) {
	fake := &recordingProvider{result: provider.Result{
		Provider: "test", ExternalID: "clip-1", Title: "Resolved quote", Kind: media.KindVideo,
		QuoteMatch: &provider.QuoteMatch{Text: "we shipped it", StartMS: 1200, EndMS: 3400, Exact: true, Confidence: 1},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/test/items/clip-1/quote?q=we+shipped+it&locale=en", nil)
	response := httptest.NewRecorder()
	New(Options{Planner: planner.Local{}, Providers: []provider.Provider{fake}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if fake.resolvedID != "clip-1" || fake.resolvedLocale != "en" || fake.resolvedQuote != "we shipped it" {
		t.Fatalf("resolved ID = %q; locale = %q; quote = %q", fake.resolvedID, fake.resolvedLocale, fake.resolvedQuote)
	}
	var result provider.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.QuoteMatch == nil || result.QuoteMatch.StartMS != 1200 {
		t.Fatalf("result = %#v", result)
	}
}

type recordingSaver struct {
	generated media.GeneratedAsset
}

type inertIdentityProvider struct{}

func (inertIdentityProvider) AuthorizationURL(state, nonce string) string {
	return "https://identity.example/authorize?state=" + url.QueryEscape(state) + "&nonce=" + url.QueryEscape(nonce)
}

func (inertIdentityProvider) Exchange(context.Context, string, string) (account.Identity, error) {
	return account.Identity{}, errors.New("not implemented")
}

type recordingGeneratedReader struct {
	id   string
	data []byte
}

type recordingModelReader struct {
	id   string
	data []byte
}

func (r *recordingGeneratedReader) OpenGenerated(_ context.Context, id string) (media.Asset, io.ReadCloser, error) {
	r.id = id
	return media.Asset{ID: id, Provenance: media.Provenance{Generator: "test-engine"}}, io.NopCloser(bytes.NewReader(r.data)), nil
}

func (r *recordingModelReader) OpenModel(_ context.Context, id string) (media.Asset, io.ReadCloser, error) {
	r.id = id
	return media.Asset{ID: id, Kind: media.KindModel}, io.NopCloser(bytes.NewReader(r.data)), nil
}

type recordingProvider struct {
	query          provider.Query
	result         provider.Result
	resolvedID     string
	resolvedLocale string
	resolvedQuote  string
}

func (p *recordingProvider) Resolve(_ context.Context, externalID, locale string) (provider.Result, error) {
	p.resolvedID = externalID
	p.resolvedLocale = locale
	return p.result, nil
}

func (p *recordingProvider) ResolveQuote(_ context.Context, externalID, locale, quote string) (provider.Result, error) {
	p.resolvedID = externalID
	p.resolvedLocale = locale
	p.resolvedQuote = quote
	return p.result, nil
}

type recordingImageGenerator struct {
	request    imagegen.Request
	descriptor imagegen.Descriptor
	err        error
}

type recordingModelGenerator struct {
	request modelgen.Request
	result  modelgen.Result
	err     error
}

type recordingCinematicRenderer struct {
	request cinematic.Request
}

type recordingVideoDecoder struct {
	request video.Request
}

func (d *recordingVideoDecoder) Descriptor() video.Descriptor {
	return video.Descriptor{ID: "test-video", Label: "Test video", Local: true}
}

func (d *recordingVideoDecoder) Decode(_ context.Context, request video.Request) (*gif.GIF, error) {
	d.request = request
	animation := &gif.GIF{Config: image.Config{Width: 8, Height: 8}, LoopCount: 0}
	for frameNumber := range request.Frames {
		frame := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White})
		frame.SetColorIndex(frameNumber%8, frameNumber%8, 1)
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, 5)
	}
	return animation, nil
}

func (g *recordingImageGenerator) Descriptor() imagegen.Descriptor {
	if g.descriptor.ID != "" {
		return g.descriptor
	}
	return imagegen.Descriptor{ID: "test-local", Label: "Test local", Local: true, Semantic: true, SupportsReferences: true}
}

func (g *recordingImageGenerator) Generate(_ context.Context, request imagegen.Request) (imagegen.Result, error) {
	g.request = request
	if g.err != nil {
		return imagegen.Result{}, g.err
	}
	return imagegen.Result{Data: generatedTestPNG(nil), ContentType: "image/png", Engine: "test-local"}, nil
}

func (g *recordingModelGenerator) Descriptor() modelgen.Descriptor {
	return modelgen.Descriptor{ID: "recording-3d", Label: "Recording 3D", Recipes: []modelgen.Recipe{{ID: "tripo-3.1", Label: "Tripo 3.1"}}}
}

func (g *recordingModelGenerator) Generate(_ context.Context, request modelgen.Request) (modelgen.Result, error) {
	g.request = request
	return g.result, g.err
}

func (r *recordingCinematicRenderer) Descriptor() cinematic.Descriptor {
	return cinematic.Descriptor{ID: "cinematic-local", Label: "Test cinematic", Local: true, Enabled: true, SupportsReferences: true}
}

func (r *recordingCinematicRenderer) Render(_ context.Context, request cinematic.Request) (cinematic.Result, error) {
	r.request = request
	animation := &gif.GIF{LoopCount: 0}
	for frameNumber := range request.Spec.Frames {
		frame := image.NewPaletted(image.Rect(0, 0, request.Spec.Width, request.Spec.Height), color.Palette{color.Black, color.White})
		frame.SetColorIndex(frameNumber, frameNumber, 1)
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, request.Spec.DelayMS/10)
	}
	var output bytes.Buffer
	if err := gif.EncodeAll(&output, animation); err != nil {
		return cinematic.Result{}, err
	}
	return cinematic.Result{Data: output.Bytes(), ContentType: "image/gif", Engine: "blender+unity-6.3+unreal-5+ffmpeg"}, nil
}

func generatedTestPNG(t *testing.T) []byte {
	still := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			still.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 90, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, still); err != nil {
		if t != nil {
			t.Fatal(err)
		}
		panic(err)
	}
	return output.Bytes()
}

func (p *recordingProvider) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "test", Label: "Test provider"}
}

func (p *recordingProvider) Search(_ context.Context, query provider.Query) (provider.Page, error) {
	p.query = query
	return provider.Page{Provider: "test", Results: []provider.Result{{Provider: "test", ExternalID: "result-1", Title: "Victory"}}}, nil
}

func (s *recordingSaver) SaveGenerated(_ context.Context, generated media.GeneratedAsset) (media.Asset, error) {
	s.generated = generated
	return media.Asset{ID: "gif_saved"}, nil
}

type recordingModelSaver struct {
	generated media.GeneratedModel
}

func (s *recordingModelSaver) SaveModel(_ context.Context, generated media.GeneratedModel) (media.Asset, error) {
	s.generated = generated
	return media.Asset{ID: "model_saved", Kind: media.KindModel}, nil
}
