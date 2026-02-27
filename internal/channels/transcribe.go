package channels

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// GeminiTranscriber uses the Gemini API to transcribe audio.
type GeminiTranscriber struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGeminiTranscriber creates a transcriber using the Gemini generateContent API.
func NewGeminiTranscriber(apiKey, model string) *GeminiTranscriber {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &GeminiTranscriber{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *GeminiTranscriber) Transcribe(_ context.Context, audioData []byte, mimeType string) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(audioData)

	reqBody := map[string]any{
		"contents": []map[string]any{{
			"parts": []map[string]any{
				{"text": "Transcribe this audio exactly. Return only the transcribed text, nothing else."},
				{"inline_data": map[string]string{
					"mime_type": mimeType,
					"data":      b64,
				}},
			},
		}},
	}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", g.model, g.apiKey)

	resp, err := g.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		return result.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("no transcription in response")
}

// GeminiTTS uses the Gemini TTS API to synthesize speech.
type GeminiTTS struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGeminiTTS creates a TTS synthesizer.
func NewGeminiTTS(apiKey, model string) *GeminiTTS {
	if model == "" {
		model = "gemini-2.5-flash-preview-tts"
	}
	return &GeminiTTS{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Synthesize converts text to OGG audio bytes.
func (g *GeminiTTS) Synthesize(_ context.Context, text string) ([]byte, error) {
	// Trim to reasonable length for TTS
	if len(text) > 3000 {
		text = text[:3000]
	}

	// Strip markdown formatting for cleaner speech
	text = stripMarkdown(text)

	reqBody := map[string]any{
		"contents": []map[string]any{{
			"parts": []map[string]any{
				{"text": text},
			},
		}},
		"generationConfig": map[string]any{
			"response_modalities": []string{"AUDIO"},
			"speech_config": map[string]any{
				"voice_config": map[string]any{
					"prebuilt_voice_config": map[string]string{
						"voice_name": "Kore",
					},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", g.model, g.apiKey)

	resp, err := g.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini TTS request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading TTS response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini TTS error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing TTS response: %w", err)
	}

	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		part := result.Candidates[0].Content.Parts[0]
		if part.InlineData != nil && part.InlineData.Data != "" {
			pcmData, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				return nil, fmt.Errorf("decoding audio: %w", err)
			}

			// Convert raw PCM (16-bit, 24kHz, mono) to OGG Opus for Telegram
			ogg, err := pcmToOGG(pcmData, 24000)
			if err != nil {
				return nil, fmt.Errorf("converting to ogg: %w", err)
			}
			return ogg, nil
		}
	}

	return nil, fmt.Errorf("no audio in TTS response")
}

// pcmToOGG converts raw PCM (16-bit signed, mono) to OGG Opus using ffmpeg.
func pcmToOGG(pcm []byte, sampleRate int) ([]byte, error) {
	// Build WAV header for the PCM data so ffmpeg can read it
	wav := pcmToWAV(pcm, sampleRate, 1, 16)

	cmd := exec.Command("ffmpeg",
		"-i", "pipe:0",
		"-c:a", "libopus",
		"-b:a", "64k",
		"-f", "ogg",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(wav)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// pcmToWAV wraps raw PCM data with a WAV header.
func pcmToWAV(pcm []byte, sampleRate, channels, bitsPerSample int) []byte {
	dataSize := len(pcm)
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, int32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, int32(16))           // chunk size
	binary.Write(&buf, binary.LittleEndian, int16(1))            // PCM format
	binary.Write(&buf, binary.LittleEndian, int16(channels))     // channels
	binary.Write(&buf, binary.LittleEndian, int32(sampleRate))   // sample rate
	binary.Write(&buf, binary.LittleEndian, int32(byteRate))     // byte rate
	binary.Write(&buf, binary.LittleEndian, int16(blockAlign))   // block align
	binary.Write(&buf, binary.LittleEndian, int16(bitsPerSample)) // bits per sample
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, int32(dataSize))
	buf.Write(pcm)

	return buf.Bytes()
}

// stripMarkdown removes common markdown formatting for cleaner TTS.
func stripMarkdown(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "```", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "###", "")
	s = strings.ReplaceAll(s, "##", "")
	s = strings.ReplaceAll(s, "# ", "")
	return strings.TrimSpace(s)
}
