package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunUsesPositionalArgsBeforeStdin(t *testing.T) {
	t.Setenv(apiKeyEnv, " test-key ")
	var capturedText string
	var capturedAPIKey string
	stubGenerateSpeech(t, func(_ context.Context, apiKey string, options synthesisOptions, text string) ([]byte, error) {
		capturedAPIKey = apiKey
		capturedText = text
		assert.Equal(t, defaultModelName, options.Model)
		assert.Equal(t, "Kore", options.Voice)
		assert.Equal(t, "wav", options.Format.Name)
		return []byte("audio"), nil
	})

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"--output", "-", "hello", "world"}, bytes.NewBufferString("ignored stdin"), &stdout, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Equal(t, "test-key", capturedAPIKey)
	assert.Equal(t, "hello world", capturedText)
	assert.Equal(t, "audio", stdout.String())
}

func TestRunFallsBackToStdin(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	var capturedText string
	stubGenerateSpeech(t, func(_ context.Context, _ string, _ synthesisOptions, text string) ([]byte, error) {
		capturedText = text
		return []byte("audio"), nil
	})

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"--output", "-"}, bytes.NewBufferString("\n  from stdin  \n"), &stdout, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Equal(t, "from stdin", capturedText)
	assert.Equal(t, "audio", stdout.String())
}

func TestRunErrorsWhenTextIsEmpty(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	stubGenerateSpeech(t, func(context.Context, string, synthesisOptions, string) ([]byte, error) {
		t.Fatal("generateSpeechFunc should not be called")
		return nil, nil
	})

	err := run(context.Background(), []string{"--output", "-"}, bytes.NewBufferString(" \n\t "), &bytes.Buffer{}, &bytes.Buffer{})

	require.EqualError(t, err, "text is empty; pass text as arguments or stdin")
}

func TestRunListVoicesDoesNotRequireAPIKey(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"--list-voices"}, bytes.NewBuffer(nil), &stdout, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Kore\tFirm")
	assert.Contains(t, stdout.String(), "Zephyr\tBright")
}

func TestRunBuildsTwoSpeakerOptions(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	var capturedOptions synthesisOptions
	var capturedText string
	stubGenerateSpeech(t, func(_ context.Context, _ string, options synthesisOptions, text string) ([]byte, error) {
		capturedOptions = options
		capturedText = text
		return []byte("audio"), nil
	})

	err := run(context.Background(), []string{
		"--output", "-",
		"--speaker1", "Alice",
		"--voice1", "kore",
		"--speaker2", "Bob",
		"--voice2", "puck",
		"Alice:", "Hi",
	}, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Equal(t, "Alice: Hi", capturedText)
	assert.Equal(t, "Alice", capturedOptions.Speaker1)
	assert.Equal(t, "Kore", capturedOptions.Voice1)
	assert.Equal(t, "Bob", capturedOptions.Speaker2)
	assert.Equal(t, "Puck", capturedOptions.Voice2)
}

func TestRunReturnsGenerateSpeechError(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	wantErr := errors.New("api failed")
	stubGenerateSpeech(t, func(context.Context, string, synthesisOptions, string) ([]byte, error) {
		return nil, wantErr
	})

	err := run(context.Background(), []string{"--output", "-", "hello"}, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{})

	require.ErrorIs(t, err, wantErr)
}

func TestRunValidatesFlagsBeforeAPIKey(t *testing.T) {
	err := run(context.Background(), []string{"--voice", "unknown", "hello"}, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{})

	require.EqualError(t, err, "unsupported voice \"unknown\"; run with --list-voices to see supported voices")
}

func TestParseAudioFormat(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want audioFormat
	}{
		{name: "wav", in: " WAV ", want: audioFormat{Name: "wav", Enum: "AUDIO_WAV"}},
		{name: "pcm alias", in: "l16", want: audioFormat{Name: "pcm", Enum: "AUDIO_L16"}},
		{name: "opus alias", in: "opus", want: audioFormat{Name: "ogg-opus", Enum: "AUDIO_OGG_OPUS"}},
		{name: "a-law alias", in: "a-law", want: audioFormat{Name: "alaw", Enum: "AUDIO_ALAW"}},
		{name: "mu-law alias", in: "mu-law", want: audioFormat{Name: "mulaw", Enum: "AUDIO_MULAW"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAudioFormat(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	_, err := parseAudioFormat("flac")
	require.EqualError(t, err, "unsupported format \"flac\"; use wav, pcm, mp3, ogg-opus, alaw, or mulaw")
}

func TestBuildSpeechConfig(t *testing.T) {
	single := buildSpeechConfig(synthesisOptions{Voice: "Aoede", Language: "en-US"})
	require.NotNil(t, single.VoiceConfig)
	assert.Equal(t, "en-US", single.LanguageCode)
	assert.Equal(t, "Aoede", single.VoiceConfig.PrebuiltVoiceConfig.VoiceName)
	assert.Nil(t, single.MultiSpeakerVoiceConfig)

	multi := buildSpeechConfig(synthesisOptions{Speaker1: "Alice", Voice1: "Kore", Speaker2: "Bob", Voice2: "Puck"})
	require.NotNil(t, multi.MultiSpeakerVoiceConfig)
	assert.Nil(t, multi.VoiceConfig)
	assert.Equal(t, []speakerVoiceConfig{
		{Speaker: "Alice", VoiceConfig: voiceConfig{PrebuiltVoiceConfig: prebuiltVoiceConfig{VoiceName: "Kore"}}},
		{Speaker: "Bob", VoiceConfig: voiceConfig{PrebuiltVoiceConfig: prebuiltVoiceConfig{VoiceName: "Puck"}}},
	}, multi.MultiSpeakerVoiceConfig.SpeakerVoiceConfigs)
}

func TestFormatAudioOutputConvertsPCMToWAV(t *testing.T) {
	pcm := []byte{0x01, 0x00, 0x02, 0x00}
	got, err := formatAudioOutput(pcm, "audio/L16", synthesisOptions{Format: audioFormat{Name: "wav"}, SampleRate: 16000})

	require.NoError(t, err)
	require.Len(t, got, 48)
	assert.Equal(t, "RIFF", string(got[:4]))
	assert.Equal(t, uint32(40), binary.LittleEndian.Uint32(got[4:8]))
	assert.Equal(t, "WAVE", string(got[8:12]))
	assert.Equal(t, uint32(16000), binary.LittleEndian.Uint32(got[24:28]))
	assert.Equal(t, pcm, got[44:])
}

func TestFormatAudioOutputRejectsUnalignedPCM(t *testing.T) {
	_, err := formatAudioOutput([]byte{0x01}, "audio/L16", synthesisOptions{Format: audioFormat{Name: "wav"}})

	require.EqualError(t, err, "PCM data length is not aligned to 16-bit samples")
}

func TestFormatAudioOutputLeavesNonWAVUnchanged(t *testing.T) {
	audio := []byte("encoded")
	got, err := formatAudioOutput(audio, "audio/mpeg", synthesisOptions{Format: audioFormat{Name: "mp3"}})

	require.NoError(t, err)
	assert.Equal(t, audio, got)
}

func TestApplyStyle(t *testing.T) {
	assert.Equal(t, "hello", applyStyle("", "hello"))
	assert.Equal(t, "Generate speech using these style instructions: warmly\n\nSpoken transcript:\nhello", applyStyle("warmly", "hello"))
}

func stubGenerateSpeech(t *testing.T, fn func(context.Context, string, synthesisOptions, string) ([]byte, error)) {
	t.Helper()
	original := generateSpeechFunc
	generateSpeechFunc = fn
	t.Cleanup(func() {
		generateSpeechFunc = original
	})
}
