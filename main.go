package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultModelName = "gemini-3.1-flash-tts-preview"
	apiKeyEnv        = "GEMINI_API_KEY"
	sampleRateHz     = 24000
	channelCount     = 1
	bitsPerSample    = 16
)

var voices = map[string]string{
	"Achernar":      "Soft",
	"Achird":        "Friendly",
	"Algenib":       "Gravelly",
	"Algieba":       "Smooth",
	"Alnilam":       "Firm",
	"Aoede":         "Breezy",
	"Autonoe":       "Bright",
	"Callirrhoe":    "Easy-going",
	"Charon":        "Informative",
	"Despina":       "Smooth",
	"Enceladus":     "Breathy",
	"Erinome":       "Clear",
	"Fenrir":        "Excitable",
	"Gacrux":        "Mature",
	"Iapetus":       "Clear",
	"Kore":          "Firm",
	"Laomedeia":     "Upbeat",
	"Leda":          "Youthful",
	"Orus":          "Firm",
	"Puck":          "Upbeat",
	"Pulcherrima":   "Forward",
	"Rasalgethi":    "Informative",
	"Sadachbia":     "Lively",
	"Sadaltager":    "Knowledgeable",
	"Schedar":       "Even",
	"Sulafat":       "Warm",
	"Umbriel":       "Easy-going",
	"Vindemiatrix":  "Gentle",
	"Zephyr":        "Bright",
	"Zubenelgenubi": "Casual",
}

var generateSpeechFunc = generateSpeech

type generateRequest struct {
	Contents         []content        `json:"contents"`
	GenerationConfig generationConfig `json:"generationConfig"`
	Model            string           `json:"model"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text,omitempty"`
}

type generationConfig struct {
	ResponseModalities []string              `json:"responseModalities"`
	SpeechConfig       speechConfig          `json:"speechConfig"`
	ResponseFormat     *responseFormatConfig `json:"responseFormat,omitempty"`
}

type speechConfig struct {
	VoiceConfig             *voiceConfig             `json:"voiceConfig,omitempty"`
	MultiSpeakerVoiceConfig *multiSpeakerVoiceConfig `json:"multiSpeakerVoiceConfig,omitempty"`
	LanguageCode            string                   `json:"languageCode,omitempty"`
}

type voiceConfig struct {
	PrebuiltVoiceConfig prebuiltVoiceConfig `json:"prebuiltVoiceConfig"`
}

type prebuiltVoiceConfig struct {
	VoiceName string `json:"voiceName"`
}

type multiSpeakerVoiceConfig struct {
	SpeakerVoiceConfigs []speakerVoiceConfig `json:"speakerVoiceConfigs"`
}

type speakerVoiceConfig struct {
	Speaker     string      `json:"speaker"`
	VoiceConfig voiceConfig `json:"voiceConfig"`
}

type responseFormatConfig struct {
	Audio *audioResponseFormat `json:"audio,omitempty"`
}

type audioResponseFormat struct {
	MIMEType   string `json:"mimeType,omitempty"`
	Delivery   string `json:"delivery,omitempty"`
	SampleRate int    `json:"sampleRate,omitempty"`
	BitRate    int    `json:"bitRate,omitempty"`
}

type synthesisOptions struct {
	Model             string
	Voice             string
	Format            audioFormat
	SampleRate        int
	BitRate           int
	Language          string
	Style             string
	Speaker1          string
	Voice1            string
	Speaker2          string
	Voice2            string
	UseResponseFormat bool
}

type audioFormat struct {
	Name string
	Enum string
}

type generateResponse struct {
	Candidates []candidate `json:"candidates"`
	Error      *apiError   `json:"error,omitempty"`
}

type candidate struct {
	Content responseContent `json:"content"`
}

type responseContent struct {
	Parts []responsePart `json:"parts"`
}

type responsePart struct {
	InlineData *inlineData `json:"inlineData,omitempty"`
	Text       string      `json:"text,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("gemini-say", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printHelp(stderr, flags) }

	voice := flags.String("voice", "Kore", "Gemini TTS voice name")
	model := flags.String("model", defaultModelName, "Gemini model name")
	format := flags.String("format", "wav", "audio output format: wav, pcm, mp3, ogg-opus, alaw, or mulaw")
	sampleRate := flags.Int("sample-rate", sampleRateHz, "audio sample rate in Hz")
	bitRate := flags.Int("bit-rate", 0, "audio bit rate in bps for compressed formats")
	language := flags.String("language", "", "BCP-47 language code, such as en-US or ja-JP")
	style := flags.String("style", "", "style guidance prepended to the input text")
	speaker1 := flags.String("speaker1", "", "first speaker name for two-speaker TTS")
	voice1 := flags.String("voice1", "Kore", "first speaker Gemini TTS voice name")
	speaker2 := flags.String("speaker2", "", "second speaker name for two-speaker TTS")
	voice2 := flags.String("voice2", "Puck", "second speaker Gemini TTS voice name")
	output := flags.String("output", "-", "audio output path, or - for stdout")
	listVoices := flags.Bool("list-voices", false, "list supported Gemini TTS voice names")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *listVoices {
		printVoices(stdout)
		return nil
	}

	modelName := strings.TrimSpace(*model)
	if modelName == "" {
		return errors.New("model is empty")
	}
	audioFormat, err := parseAudioFormat(*format)
	if err != nil {
		return err
	}
	if *sampleRate < 0 {
		return errors.New("sample-rate cannot be negative")
	}
	if *bitRate < 0 {
		return errors.New("bit-rate cannot be negative")
	}

	multiSpeaker := flagProvided(args, "speaker1", "speaker2", "voice1", "voice2")
	formatProvided := flagProvided(args, "format")
	sampleRateProvided := flagProvided(args, "sample-rate")
	bitRateProvided := flagProvided(args, "bit-rate")
	options := synthesisOptions{
		Model:             modelName,
		Format:            audioFormat,
		SampleRate:        *sampleRate,
		BitRate:           *bitRate,
		Language:          strings.TrimSpace(*language),
		Style:             strings.TrimSpace(*style),
		UseResponseFormat: (formatProvided && audioFormat.Name != "wav" && audioFormat.Name != "pcm") || sampleRateProvided || bitRateProvided,
	}
	var ok bool

	if multiSpeaker {
		options.Speaker1 = strings.TrimSpace(*speaker1)
		options.Speaker2 = strings.TrimSpace(*speaker2)
		if options.Speaker1 == "" || options.Speaker2 == "" {
			return errors.New("two-speaker mode requires both --speaker1 and --speaker2")
		}
		options.Voice1, ok = canonicalVoiceName(*voice1)
		if !ok {
			return fmt.Errorf("unsupported voice1 %q; run with --list-voices to see supported voices", *voice1)
		}
		options.Voice2, ok = canonicalVoiceName(*voice2)
		if !ok {
			return fmt.Errorf("unsupported voice2 %q; run with --list-voices to see supported voices", *voice2)
		}
	} else {
		options.Voice, ok = canonicalVoiceName(*voice)
		if !ok {
			return fmt.Errorf("unsupported voice %q; run with --list-voices to see supported voices", *voice)
		}
	}

	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnv))
	if apiKey == "" {
		return fmt.Errorf("%s is not set", apiKeyEnv)
	}

	text := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if text == "" {
		input, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		text = strings.TrimSpace(string(input))
	}
	if text == "" {
		return errors.New("text is empty; pass text as arguments or stdin")
	}

	audio, err := generateSpeechFunc(ctx, apiKey, options, text)
	if err != nil {
		return err
	}

	if shouldWriteToStdout(*output, stdout) {
		_, err = stdout.Write(audio)
		return err
	}

	return os.WriteFile(*output, audio, 0o644)
}

func printHelp(w io.Writer, flags *flag.FlagSet) {
	fmt.Fprintln(w, "gemini-say")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Reads text from arguments or stdin, sends it to Gemini TTS, and writes audio to a file or stdout.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gemini-say [flags] \"text to speak\"")
	fmt.Fprintln(w, "  gemini-say [flags] < input.txt")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Environment:")
	fmt.Fprintf(w, "  %s must contain your Gemini API key.\n", apiKeyEnv)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	flags.PrintDefaults()
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  # Single-speaker WAV file")
	fmt.Fprintln(w, "  gemini-say --voice Kore --output out.wav 'Welcome to the demo.'")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  # Read text from stdin")
	fmt.Fprintln(w, "  echo 'Welcome to the demo.' | gemini-say --voice Kore --output out.wav")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  # Override the default model explicitly")
	fmt.Fprintln(w, "  gemini-say --model gemini-3.1-flash-tts-preview --voice Zephyr --output out.wav 'Have a wonderful day!'")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  # Prompt style controls pace, pitch, emotion, accent, and pauses")
	fmt.Fprintln(w, "  echo 'By the pricking of my thumbs, something wicked this way comes.' | gemini-say --voice Enceladus --style 'Speak slowly in a spooky whisper with short pauses' --output spooky.wav")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  # Two-speaker conversation; speaker names must match the transcript")
	fmt.Fprintln(w, "  cat conversation.txt | gemini-say --speaker1 Alice --voice1 Kore --speaker2 Bob --voice2 Puck --output conversation.wav")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  # Compressed output, if supported by the selected model")
	fmt.Fprintln(w, "  echo 'Short announcement.' | gemini-say --format mp3 --bit-rate 64000 --output announcement.mp3")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  # Redirected or piped stdout takes precedence over --output")
	fmt.Fprintln(w, "  echo 'Pipe me.' | gemini-say --output ignored.wav > actual.wav")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Prompting tips:")
	fmt.Fprintln(w, "  - Keep the actual spoken transcript clear and separate from stage directions.")
	fmt.Fprintln(w, "  - Use --style for delivery guidance such as 'slow', 'excited', 'warm', 'lower pitch', or '[short pause]'.")
	fmt.Fprintln(w, "  - For two speakers, include lines like 'Alice: ...' and 'Bob: ...' in stdin and pass matching --speaker1/--speaker2 names.")
	fmt.Fprintln(w, "  - Long transcripts may drift in voice quality; split very long input into smaller files.")
	fmt.Fprintln(w, "  - Run --list-voices to see supported voice names.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Notes:")
	fmt.Fprintf(w, "  - The default model is %s. If you override it, choose a TTS-capable model for audio output.\n", defaultModelName)
	fmt.Fprintln(w, "  - Gemini TTS generateContent responses are not streaming; audio is written after the API response completes.")
}

func shouldWriteToStdout(output string, stdout io.Writer) bool {
	if output == "-" {
		return true
	}

	file, ok := stdout.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice == 0
}

func canonicalVoiceName(name string) (string, bool) {
	for voice := range voices {
		if strings.EqualFold(name, voice) {
			return voice, true
		}
	}
	return "", false
}

func printVoices(w io.Writer) {
	order := []string{
		"Zephyr", "Puck", "Charon", "Kore", "Fenrir", "Leda", "Orus", "Aoede", "Callirrhoe", "Autonoe",
		"Enceladus", "Iapetus", "Umbriel", "Algieba", "Despina", "Erinome", "Algenib", "Rasalgethi",
		"Laomedeia", "Achernar", "Alnilam", "Schedar", "Gacrux", "Pulcherrima", "Achird", "Zubenelgenubi",
		"Vindemiatrix", "Sadachbia", "Sadaltager", "Sulafat",
	}

	for _, voice := range order {
		fmt.Fprintf(w, "%s\t%s\n", voice, voices[voice])
	}
}

func flagProvided(args []string, names ...string) bool {
	for _, arg := range args {
		for _, name := range names {
			if arg == "-"+name || arg == "--"+name || strings.HasPrefix(arg, "-"+name+"=") || strings.HasPrefix(arg, "--"+name+"=") {
				return true
			}
		}
	}
	return false
}

func parseAudioFormat(format string) (audioFormat, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return audioFormat{Name: "wav", Enum: "AUDIO_WAV"}, nil
	case "pcm", "l16":
		return audioFormat{Name: "pcm", Enum: "AUDIO_L16"}, nil
	case "mp3":
		return audioFormat{Name: "mp3", Enum: "AUDIO_MP3"}, nil
	case "ogg-opus", "opus":
		return audioFormat{Name: "ogg-opus", Enum: "AUDIO_OGG_OPUS"}, nil
	case "alaw", "a-law":
		return audioFormat{Name: "alaw", Enum: "AUDIO_ALAW"}, nil
	case "mulaw", "mu-law":
		return audioFormat{Name: "mulaw", Enum: "AUDIO_MULAW"}, nil
	default:
		return audioFormat{}, fmt.Errorf("unsupported format %q; use wav, pcm, mp3, ogg-opus, alaw, or mulaw", format)
	}
}

func generateSpeech(ctx context.Context, apiKey string, options synthesisOptions, text string) ([]byte, error) {
	text = applyStyle(options.Style, text)
	speechConfig := buildSpeechConfig(options)
	body := generateRequest{
		Contents: []content{{Parts: []part{{Text: text}}}},
		GenerationConfig: generationConfig{
			ResponseModalities: []string{"AUDIO"},
			SpeechConfig:       speechConfig,
		},
		Model: options.Model,
	}
	if options.UseResponseFormat {
		body.GenerationConfig.ResponseFormat = &responseFormatConfig{
			Audio: &audioResponseFormat{
				MIMEType:   options.Format.Enum,
				Delivery:   "INLINE",
				SampleRate: options.SampleRate,
				BitRate:    options.BitRate,
			},
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", options.Model)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call Gemini API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read Gemini API response: %w", err)
	}

	var decoded generateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode Gemini API response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if decoded.Error != nil && decoded.Error.Message != "" {
			if options.UseResponseFormat && resp.StatusCode == http.StatusBadRequest {
				return nil, fmt.Errorf("Gemini API returned %s: %s; the selected model may not support --format, --sample-rate, or --bit-rate responseFormat options", resp.Status, decoded.Error.Message)
			}
			return nil, fmt.Errorf("Gemini API returned %s: %s", resp.Status, decoded.Error.Message)
		}
		return nil, fmt.Errorf("Gemini API returned %s", resp.Status)
	}

	if len(decoded.Candidates) == 0 || len(decoded.Candidates[0].Content.Parts) == 0 || decoded.Candidates[0].Content.Parts[0].InlineData == nil {
		return nil, missingAudioError(decoded, options)
	}

	inline := decoded.Candidates[0].Content.Parts[0].InlineData
	audio, err := base64.StdEncoding.DecodeString(inline.Data)
	if err != nil {
		return nil, fmt.Errorf("decode audio data: %w", err)
	}

	if len(audio) == 0 {
		return nil, errors.New("Gemini API returned empty audio data")
	}

	return formatAudioOutput(audio, inline.MimeType, options)
}

func missingAudioError(response generateResponse, options synthesisOptions) error {
	if len(response.Candidates) > 0 {
		for _, part := range response.Candidates[0].Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				return fmt.Errorf("model %q returned text instead of audio; pass a TTS-capable model with --model, such as gemini-3.1-flash-tts-preview", options.Model)
			}
		}
	}

	return fmt.Errorf("model %q did not return inline audio data; pass a TTS-capable model with --model, such as gemini-3.1-flash-tts-preview", options.Model)
}

func applyStyle(style string, text string) string {
	if style == "" {
		return text
	}
	return fmt.Sprintf("Generate speech using these style instructions: %s\n\nSpoken transcript:\n%s", style, text)
}

func buildSpeechConfig(options synthesisOptions) speechConfig {
	config := speechConfig{LanguageCode: options.Language}
	if options.Speaker1 != "" || options.Speaker2 != "" {
		config.MultiSpeakerVoiceConfig = &multiSpeakerVoiceConfig{
			SpeakerVoiceConfigs: []speakerVoiceConfig{
				{
					Speaker: options.Speaker1,
					VoiceConfig: voiceConfig{
						PrebuiltVoiceConfig: prebuiltVoiceConfig{VoiceName: options.Voice1},
					},
				},
				{
					Speaker: options.Speaker2,
					VoiceConfig: voiceConfig{
						PrebuiltVoiceConfig: prebuiltVoiceConfig{VoiceName: options.Voice2},
					},
				},
			},
		}
		return config
	}

	config.VoiceConfig = &voiceConfig{
		PrebuiltVoiceConfig: prebuiltVoiceConfig{VoiceName: options.Voice},
	}
	return config
}

func formatAudioOutput(audio []byte, mimeType string, options synthesisOptions) ([]byte, error) {
	if options.Format.Name != "wav" {
		return audio, nil
	}
	if bytes.HasPrefix(audio, []byte("RIFF")) || strings.Contains(strings.ToLower(mimeType), "wav") {
		return audio, nil
	}
	rate := options.SampleRate
	if rate == 0 {
		rate = sampleRateHz
	}
	return pcmToWAV(audio, rate)
}

func pcmToWAV(pcm []byte, sampleRate int) ([]byte, error) {
	if len(pcm)%2 != 0 {
		return nil, errors.New("PCM data length is not aligned to 16-bit samples")
	}

	var out bytes.Buffer
	byteRate := sampleRate * channelCount * bitsPerSample / 8
	blockAlign := channelCount * bitsPerSample / 8
	dataSize := uint32(len(pcm))
	chunkSize := uint32(36 + len(pcm))

	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, chunkSize)
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	binary.Write(&out, binary.LittleEndian, uint32(16))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(channelCount))
	binary.Write(&out, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&out, binary.LittleEndian, uint32(byteRate))
	binary.Write(&out, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&out, binary.LittleEndian, uint16(bitsPerSample))
	out.WriteString("data")
	binary.Write(&out, binary.LittleEndian, dataSize)
	out.Write(pcm)

	return out.Bytes(), nil
}
