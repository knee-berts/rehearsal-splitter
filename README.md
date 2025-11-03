# Rehearsal Splitter

A Go-based command-line tool to automatically split a single, long video or audio file (like a band practice or live show) into individual songs, removing the "in-between" parts.

-----

## 🎵 The Problem

You have a two-hour-long video of your band's practice. It contains 10-15 songs, but also a lot of talking, tuning, and noodling in between. Manually editing this file to get a separate video for each song is tedious and time-consuming.

This tool automates that process.

## ⚙️ How It Works

This tool is a smart "controller" for the powerful **FFmpeg** multimedia framework. It works in two phases:

1.  **Detect Breaks:** It scans the audio track of your video, not for pure *silence*, but for periods of *quiet*. You define a decibel level (e.g., `-20dB`) that represents your "quiet" level (talking, tuning, amp hum). Any sound *below* this threshold is considered a "break," and any sound *above* it is considered a "song."
2.  **Filter & Split:** It calculates the timestamps of all the "song" segments. It then filters out any segments that are too short (e.g., a 30-second false start) to meet your `min_song_length` requirement. Finally, it uses FFmpeg to create a new, separate video file for each valid song.

Because it uses `ffmpeg`'s "copy" codec, this process is **extremely fast** and does not re-encode or reduce the quality of your video.

-----

## 🛠️ Setup

### 1\. Install FFmpeg (Required)

You must have `ffmpeg` installed and accessible in your system's PATH.

  * **macOS (easiest):**
    ```sh
    brew install ffmpeg
    ```
  * **Windows:**
    1.  Download a static build from [ffmpeg.org](https://ffmpeg.org/download.html).
    2.  Unzip it (e.g., to `C:\ffmpeg`).
    3.  Add the `bin` folder (e.g., `C:\ffmpeg\bin`) to your system's "Environment Variables" Path.
  * **Linux (Ubuntu/Debian):**
    ```sh
    sudo apt update && sudo apt install ffmpeg
    ```

To verify it's installed, open a new terminal and type `ffmpeg`. You should see version information.

### 2\. Install Go (Required)

If you don't already have it, [download and install the Go toolchain](https://go.dev/doc/install) for your operating system.

-----

## 🚀 Usage

1.  Clone this repository or download the `split_practice.go`, `split_practice_test.go`, and `go.mod` files into a new directory.

2.  Build the executable. This creates a single file (e.g., `splitter` or `splitter.exe`) that you can run.

    ```sh
    go build -o splitter .
    ```

3.  Run the tool.

      * **To use defaults (looks for `config.json`):**
        ```sh
        ./splitter
        ```
      * **To override settings with CLI flags:**
        ```sh
        ./splitter -input="live_show.mkv" -threshold="-18dB" -duration=8.0 -minsonglength=90
        ```

-----

## 🔧 Configuration

Settings are loaded in the following priority, with each step overriding the last:

1.  **Script Defaults** (hard-coded in the script)
2.  **`config.json` file** (if it exists)
3.  **CLI Flags** (always win)

### `config.json` (Optional)

You can create a `config.json` file in the same directory as the executable to save your settings.

**Example `config.json`:**

```json
{
  "input_file": "my_long_practice.mp4",
  "min_silence_duration": 8.5,
  "silence_threshold": "-22dB",
  "min_song_length": 90.0,
  "output_prefix": "MySong"
}
```

### Configuration Parameters

| Parameter | CLI Flag | Default | Description |
| :--- | :--- | :--- | :--- |
| **`input_file`** | `-input` | `"practice_session.mp4"` | The main video file you want to process. |
| **`silence_threshold`** | `-threshold` | `"-30dB"` | **The most important setting.** This is the "loudness" cutoff. Any sound *quieter* than this (e.g., -35dB) is a "break." Any sound *louder* (e.g., -25dB) is a "song." |
| **`min_silence_duration`** | `-duration` | `5.0` | The minimum time (in seconds) a "break" must last to be counted. **Decrease this** if songs with short breaks are being lumped together. |
| **`min_song_length`** | `-minsonglength`| `120.0` | The minimum time (in seconds) a "song" must be to be exported. This filters out short false starts or tuning noodles. |
| **`output_dir`** | `-output` | `"output"` | The folder where your split song files will be saved. |
| **`output_prefix`** | `-prefix` | `"Song"` | The prefix for your new files (e.g., `Song_01.mp4`, `Song_02.mp4`). |

-----

## 🧪 How to Run Tests

To validate the configuration logic and segment calculation, run:

```sh
go test -v
```