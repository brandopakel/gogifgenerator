package comfypartner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type qualityReview struct {
	Matches     bool    `json:"matches"`
	Score       float64 `json:"score"`
	Letterboxed bool    `json:"letterboxed"`
	Watermark   bool    `json:"watermark"`
	TextOverlay bool    `json:"text_overlay"`
	Collage     bool    `json:"collage"`
	Reason      string  `json:"reason"`
	RetryPrompt string  `json:"retry_prompt"`
}

func (r qualityReview) Accepted() bool {
	score := r.Score
	if score > 1 {
		score /= 100
	}
	return r.Matches && score >= 0.65 && !r.Letterboxed && !r.Watermark && !r.TextOverlay && !r.Collage
}

func retryPrompt(original string, review qualityReview) string {
	correction := strings.Join(strings.Fields(review.RetryPrompt), " ")
	if correction == "" {
		correction = strings.Join(strings.Fields(review.Reason), " ")
	}
	if correction == "" {
		correction = "make the requested concept unmistakable and fill the entire canvas without text or borders"
	}
	return original + ". Regeneration correction: " + correction
}

func (g *Generator) reviewImage(ctx context.Context, prompt string, data []byte, seed int64) (qualityReview, error) {
	if letterboxed(data) {
		return qualityReview{Reason: "large black letterbox or pillarbox bars were detected", RetryPrompt: "render edge-to-edge with no black bars"}, nil
	}
	uploaded, err := g.uploadImage(ctx, data)
	if err != nil {
		return qualityReview{}, err
	}
	workflow := validationWorkflow(prompt, uploaded, seed)
	promptID, err := g.queue(ctx, workflow)
	if err != nil {
		return qualityReview{}, err
	}
	text, err := g.waitForText(ctx, promptID)
	if err != nil {
		return qualityReview{}, err
	}
	var review qualityReview
	if err := json.Unmarshal([]byte(stripJSONFence(text)), &review); err != nil {
		return qualityReview{}, fmt.Errorf("comfy partner: decode visual quality review: %w", err)
	}
	review.Reason = strings.Join(strings.Fields(review.Reason), " ")
	review.RetryPrompt = strings.Join(strings.Fields(review.RetryPrompt), " ")
	if len(review.Reason) > 300 {
		review.Reason = review.Reason[:300]
	}
	if len(review.RetryPrompt) > 500 {
		review.RetryPrompt = review.RetryPrompt[:500]
	}
	return review, nil
}

type uploadedImage struct {
	Name      string `json:"name"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

func (g *Generator) uploadImage(ctx context.Context, data []byte) (uploadedImage, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "gogif-source.png")
	if err != nil {
		return uploadedImage{}, err
	}
	if _, err := part.Write(data); err != nil {
		return uploadedImage{}, err
	}
	_ = writer.WriteField("type", "input")
	_ = writer.WriteField("overwrite", "true")
	if err := writer.Close(); err != nil {
		return uploadedImage{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.route("/upload/image"), &body)
	if err != nil {
		return uploadedImage{}, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	g.authorize(request)
	response, err := g.client.Do(request)
	if err != nil {
		return uploadedImage{}, fmt.Errorf("%w: upload validation image: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	data, err = readLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return uploadedImage{}, err
	}
	if response.StatusCode != http.StatusOK {
		return uploadedImage{}, fmt.Errorf("%w: image upload returned HTTP %d: %s", ErrUnavailable, response.StatusCode, compactMessage(data))
	}
	var result uploadedImage
	if json.Unmarshal(data, &result) != nil || result.Name == "" || result.Type != "input" || strings.Contains(result.Name, "..") || strings.Contains(result.Subfolder, "..") {
		return uploadedImage{}, errors.New("comfy partner: image upload returned an unsafe location")
	}
	return result, nil
}

func validationWorkflow(prompt string, uploaded uploadedImage, seed int64) map[string]any {
	name := uploaded.Name
	if uploaded.Subfolder != "" {
		name = strings.Trim(uploaded.Subfolder, "/") + "/" + name
	}
	return map[string]any{
		"1": map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": name}},
		"2": map[string]any{"class_type": "ClaudeNode", "inputs": map[string]any{
			"prompt": fmt.Sprintf(`Evaluate this generated source image against the exact user request: %q.
Return only one compact JSON object with: matches (boolean), score (0 to 1), letterboxed (boolean), watermark (boolean), text_overlay (boolean), collage (boolean), reason (short string), retry_prompt (short concrete visual correction). Reject an image that depicts a different subject or setting, contains large black bars, signatures/watermarks/captions, is a collage, or only weakly implies the request. Incidental real-world signage is allowed only when natural to the requested scene.`, prompt),
			"model": "Haiku 4.5", "model.max_tokens": 4096, "model.temperature": 0,
			"seed": int(uint64(seed) & uint64(^uint32(0)>>1)), "images.image_1": []any{"1", 0},
			"system_prompt": "You are GoGIF visual QA. Be strict and factual. Return valid JSON only, with no markdown fence or commentary.",
		}},
		"3": map[string]any{"class_type": "PreviewAny", "inputs": map[string]any{"source": []any{"2", 0}}},
	}
}

func (g *Generator) waitForText(ctx context.Context, promptID string) (string, error) {
	waitContext, cancel := context.WithTimeout(ctx, g.maxWait)
	defer cancel()
	for {
		text, done, err := g.readText(waitContext, promptID)
		if err != nil {
			return "", err
		}
		if done {
			return text, nil
		}
		timer := time.NewTimer(g.pollInterval)
		select {
		case <-waitContext.Done():
			timer.Stop()
			return "", waitContext.Err()
		case <-timer.C:
		}
	}
}

func (g *Generator) readText(ctx context.Context, promptID string) (string, bool, error) {
	path := "/history/" + url.PathEscape(promptID)
	if g.cloud {
		path = "/jobs/" + url.PathEscape(promptID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.route(path), nil)
	if err != nil {
		return "", false, err
	}
	g.authorize(request)
	response, err := g.client.Do(request)
	if err != nil {
		return "", false, fmt.Errorf("%w: read quality job: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return "", false, err
	}
	if response.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if response.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("%w: quality job returned HTTP %d: %s", ErrUnavailable, response.StatusCode, compactMessage(data))
	}
	if g.cloud {
		var job cloudJob
		if err := json.Unmarshal(data, &job); err != nil {
			return "", false, fmt.Errorf("comfy partner: decode quality job: %w", err)
		}
		if text, ok := firstText(job.Outputs); ok {
			return text, true, nil
		}
		switch strings.ToLower(job.Status) {
		case "failed", "error", "cancelled":
			return "", false, fmt.Errorf("comfy partner: visual quality workflow %s: %s", job.Status, firstNonEmpty(job.ExecutionError.Message, "no text output"))
		case "completed":
			return "", false, errors.New("comfy partner: visual quality workflow completed without text")
		}
		return "", false, nil
	}
	var history map[string]localHistoryEntry
	if err := json.Unmarshal(data, &history); err != nil {
		return "", false, fmt.Errorf("comfy partner: decode local quality history: %w", err)
	}
	entry, ok := history[promptID]
	if !ok {
		return "", false, nil
	}
	if text, ok := firstText(entry.Outputs); ok {
		return text, true, nil
	}
	if entry.Status.Completed || strings.EqualFold(entry.Status.StatusStr, "error") {
		return "", false, fmt.Errorf("comfy partner: visual quality workflow completed without text: %s", localExecutionError(entry.Status.Messages))
	}
	return "", false, nil
}

func firstText(outputs map[string]outputGroup) (string, bool) {
	if output, ok := outputs["3"]; ok && len(output.Text) > 0 && strings.TrimSpace(output.Text[0]) != "" {
		return output.Text[0], true
	}
	for _, output := range outputs {
		if len(output.Text) > 0 && strings.TrimSpace(output.Text[0]) != "" {
			return output.Text[0], true
		}
	}
	return "", false
}

func stripJSONFence(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		value = strings.TrimPrefix(value, "```json")
		value = strings.TrimPrefix(value, "```")
		value = strings.TrimSuffix(strings.TrimSpace(value), "```")
	}
	start, end := strings.IndexByte(value, '{'), strings.LastIndexByte(value, '}')
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
}

func letterboxed(data []byte) bool {
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return true
	}
	bounds := decoded.Bounds()
	if bounds.Dx() < 32 || bounds.Dy() < 32 {
		return true
	}
	bandX, bandY := max(1, bounds.Dx()/12), max(1, bounds.Dy()/12)
	top := darkRatio(decoded, image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Min.Y+bandY))
	bottom := darkRatio(decoded, image.Rect(bounds.Min.X, bounds.Max.Y-bandY, bounds.Max.X, bounds.Max.Y))
	left := darkRatio(decoded, image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Min.X+bandX, bounds.Max.Y))
	right := darkRatio(decoded, image.Rect(bounds.Max.X-bandX, bounds.Min.Y, bounds.Max.X, bounds.Max.Y))
	center := darkRatio(decoded, image.Rect(bounds.Min.X+bandX*2, bounds.Min.Y+bandY*2, bounds.Max.X-bandX*2, bounds.Max.Y-bandY*2))
	return center < 0.82 && ((top > 0.92 && bottom > 0.92) || (left > 0.92 && right > 0.92))
}

func darkRatio(source image.Image, bounds image.Rectangle) float64 {
	bounds = bounds.Intersect(source.Bounds())
	if bounds.Empty() {
		return 1
	}
	dark, total := 0, 0
	step := max(1, min(bounds.Dx(), bounds.Dy())/64)
	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			r, g, b, _ := source.At(x, y).RGBA()
			luma := (299*int(r>>8) + 587*int(g>>8) + 114*int(b>>8)) / 1000
			if luma < 16 {
				dark++
			}
			total++
		}
	}
	return float64(dark) / float64(max(1, total))
}
