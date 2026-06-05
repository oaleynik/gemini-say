# gemini-say

[![CI](https://github.com/oaleynik/gemini-say/actions/workflows/ci.yml/badge.svg)](https://github.com/oaleynik/gemini-say/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/oaleynik/gemini-say)](https://github.com/oaleynik/gemini-say/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/oaleynik/gemini-say.svg)](https://pkg.go.dev/github.com/oaleynik/gemini-say)
[![Go Version](https://img.shields.io/github/go-mod/go-version/oaleynik/gemini-say)](https://github.com/oaleynik/gemini-say/blob/main/go.mod)

`gemini-say` reads text from arguments or stdin, sends it to Gemini TTS, and writes audio to a file or stdout.

It is intentionally small and Unix-friendly: pass text inline or through a pipe, choose a voice and style, and save the generated speech without starting an interactive agent.

## Demo

https://github.com/user-attachments/assets/a8700236-0e01-4e13-917f-8d78a3e7d54d

## Install

```bash
go install github.com/oaleynik/gemini-say@latest
```

## Configure

Set `GEMINI_API_KEY` to a Gemini API key:

```bash
export GEMINI_API_KEY=...
```

## Usage

```bash
gemini-say --voice Kore --output out.wav 'Welcome to the demo.'
```

You can also pipe or redirect text through stdin:

```bash
echo 'Welcome to the demo.' | gemini-say --voice Kore --output out.wav
```

If stdout is redirected or piped, audio is written to stdout and `--output` is ignored:

```bash
echo 'Pipe me.' | gemini-say --output ignored.wav > actual.wav
```

List supported voices:

```bash
gemini-say --list-voices
```

## Why gemini-say?

- Small single-purpose CLI for Gemini text-to-speech.
- Reads from positional arguments or stdin, so it works naturally in shell pipelines.
- Writes to a file or stdout for composition with other tools.
- Supports single-speaker and two-speaker Gemini TTS voices.
- Supports WAV, raw PCM, and compressed formats when the selected model supports them.

The official [Gemini CLI](https://github.com/google-gemini/gemini-cli) is a broader terminal AI agent for coding, file operations, shell commands, and MCP extensions. `gemini-say` focuses only on turning text into speech.

Other community Gemini TTS tools exist, including Python and .NET CLIs. `gemini-say` is meant to stay minimal, dependency-light, and easy to install with `go install`.

## Examples

Single-speaker WAV output:

```bash
gemini-say --voice Aoede --output out.wav 'Have a wonderful day!'
```

Prompt style controls delivery details such as pace, pitch, emotion, accent, and pauses:

```bash
echo 'By the pricking of my thumbs, something wicked this way comes.' \
  | gemini-say \
      --voice Enceladus \
      --style 'Speak slowly in a spooky whisper with short pauses' \
      --output spooky.wav
```

Two-speaker conversation. Speaker names must match the transcript:

```bash
cat conversation.txt | gemini-say \
  --speaker1 Alice --voice1 Kore \
  --speaker2 Bob --voice2 Puck \
  --output conversation.wav
```

Compressed output, if supported by the selected model:

```bash
echo 'Short announcement.' | gemini-say --format mp3 --bit-rate 64000 --output announcement.mp3
```

## Flags

Run:

```bash
gemini-say --help
```

Notable flags:

- `--model`: Gemini model name. Defaults to `gemini-3.1-flash-tts-preview`.
- `--voice`: single-speaker voice name.
- `--speaker1`, `--voice1`, `--speaker2`, `--voice2`: two-speaker mode.
- `--format`: `wav`, `pcm`, `mp3`, `ogg-opus`, `alaw`, or `mulaw`.
- `--sample-rate`: audio sample rate in Hz.
- `--bit-rate`: bitrate for compressed formats.
- `--language`: BCP-47 language code, such as `en-US` or `ja-JP`.
- `--style`: delivery guidance prepended to the input text.

## Prompting tips

- Keep the actual spoken transcript clear and separate from stage directions.
- Use `--style` for delivery guidance such as `slow`, `excited`, `warm`, `lower pitch`, or `[short pause]`.
- For two speakers, include lines like `Alice: ...` and `Bob: ...` in stdin and pass matching `--speaker1` and `--speaker2` names.
- Long transcripts may drift in voice quality; split very long input into smaller files.

Gemini TTS `generateContent` responses are not streaming; audio is written after the API response completes.
