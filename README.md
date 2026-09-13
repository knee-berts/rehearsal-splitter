# Rehearsal Splitter

A Go-based command-line tool to automatically split a single, long video or audio file (like a band practice or live show) into individual songs, removing the "in-between" parts.

-----

## 🎵 The Problem

You have a two-hour-long video of your band's practice. It contains 10-15 songs, but also a lot of talking, tuning, and noodling in between. Manually editing this file to get a separate video for each song is tedious and time-consuming.

This tool automates that process.

## ⚙️ How It Works

This tool is a smart "controller" for the powerful **FFmpeg** multimedia framework. It works in phases:

1.  **Detect Breaks:** It scans the audio track of your video, not for pure *silence*, but for periods of *quiet*. You define a decibel level (e.g., `-20dB`) that represents your "quiet" level (talking, tuning, amp hum). Any sound *below* this threshold is considered a "break," and any sound *above* it is considered a "song."

2.  **Filter & Split:** It calculates the timestamps of all the "song" segments. It then filters out any segments that are too short (e.g., a 30-second false start) to meet your `min_song_length` requirement. Finally, it uses FFmpeg to create a new, separate video file for each valid song (e.g., `Song_01.mp4`, `Song_02.mp4`).

3.  **Rename (Optional):** After splitting, it can read a `setlist.txt` file you provide to automatically rename the output files from `Song_01.mp4` to a numbered format like `01 - Your_Song_Name.mp4`.

4.  **Upload (Optional):** Finally, it can automatically upload the new video files from your `output` folder to a specified folder in your Google Drive using `rclone`.

Because it uses `ffmpeg`'s "copy" codec, the splitting process is **extremely fast** and does not re-encode or reduce the quality of your video.

-----

## Credits & Attribution

This tool is written in **Go**, but it relies on free, open-source frameworks to function:

  * **FFmpeg** ([ffmpeg.org](https://ffmpeg.org/)) **(Required)**
    All audio analysis and video splitting are performed by FFmpeg.

  * **rclone** ([rclone.org](https://rclone.org/)) **(Optional)**
    Used for the optional upload-to-cloud-storage feature.

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

### 3\. Install rclone (Optional)

If you want to use the `-upload` feature, you must also install `rclone`.

  * **macOS (easiest):**
    ```sh
    brew install rclone
    ```
  * **Windows (easiest):**
    ```sh
    choco install rclone
    ```
  * **Linux (Ubuntu/Debian):**
    ```sh
    sudo apt update && sudo apt install rclone
    ```

After installing, you must configure it one time by running `rclone config` and following the prompts to link your Google Drive (or other cloud) account.

-----

## 🚀 Usage

1.  Clone this repository and open its `code` folder in a terminal.

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
      * **To run with flags and upload to Google Drive:**
        ```sh
        ./splitter -input="show.mp4" -threshold="-20dB" -upload=true -subfolder="Band/SplitShows"
        ```
      * **To run with a setlist for renaming:**
        ```sh
        ./splitter -input="practice.mp4" -setlist="my_setlist.txt"
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
  "output_prefix": "MySong",
  "upload_to_drive": true,
  "rclone_remote": "gdrive:",
  "drive_subfolder": "Rehearsals/2025-11-03",
  "setlist_file": "my_setlist.txt"
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
| **`output_prefix`** | `-prefix` | `"Song"` | The prefix for your new files (e.g., `Song_01.mp4`). Ignored if using a setlist. |
| **`upload_to_drive`** | `-upload` | `false` | Set to `true` to enable uploading to cloud storage. |
| **`rclone_remote`** | `-remote` | `"gdrive:"` | The name of your `rclone` remote (from `rclone config`). |
| **`drive_subfolder`** | `-subfolder` | `"SplitSongs"` | The folder path inside your remote to upload to. |
| **`setlist_file`** | `-setlist` | `""` (empty) | Path to a `.txt` file for renaming. If omitted, this feature is disabled. |

### Using the Setlist Renaming Feature (Optional)

If you provide a setlist file (e.g., using `-setlist="songs.txt"`), the tool will automatically rename the split files.

1.  Create a simple `.txt` file (e.g., `songs.txt`).
2.  List **one song title per line** in the exact order you played them.

**Example `songs.txt`:**

```
Reba
Kid Charlemagne
Give Up the Funk
Sabotage
```

The tool will take its output and rename it based on this file:

  * `Song_01.mp4` → `01 - Reba.mp4`
  * `Song_02.mp4` → `02 - Kid Charlemagne.mp4`
  * `Song_03.mp4` → `03 - Give Up the Funk.mp4`
  * `Song_04.mp4` → `04 - Sabotage.mp4`

> **Note:** The script automatically sanitizes filenames, removing special characters (like `'` or `()`) and replacing spaces with underscores (`_`). If the setlist has fewer songs than the number of files created, it will only rename the files it has names for.

-----

## 🎚️ Live Shows: Board Multitrack + Video

For a show recorded on the mixing board (one WAV per channel for the whole show) and on camera (one video per set), `splitter show` builds a folder per song with every channel cut to that song, ready to mix, and then combines each finished mix with the song's video. The automated steps need only FFmpeg.

### Show folder

```
gate-city-09-11-26/
├── audio/        board channel WAVs, e.g. 01JVOX.WAV … 14OVHD2.WAV
├── video/        set videos, e.g. Set 1.MP4, Set 2.MP4
└── setlist.md    one song per line, with "SET 1:" / "SET 2:" headers
```

### 1. Prepare the songs

```sh
./splitter show prepare ../gate-city-09-11-26
```

This:

1.  **Lines up each video with the board recording** by matching the camera's audio against the board channels. It also measures the clock drift between the camera and the recorder, so no timecode or clap is needed.
2.  **Finds every song** from the instrument channels (vocal mics are ignored because they pick up talking and crowd noise), using the setlist's song counts. The longest gap is taken as the set break, and when two songs run together without a gap, they are split where the most instruments stop.
3.  **Writes one folder per song:**

    ```
    songs/
    ├── cues.csv                     song boundaries (editable)
    ├── sync.json                    where each video sits on the board recording
    └── 07 - Creep/
        ├── channels/                every board channel, cut to the song
        ├── 07 - Creep (camera).mp4  the song's video with the camera audio
        └── mix/                     put your mixdown here
    ```

    The channel WAVs and the camera video all start at the same instant, so they line up at 0:00 when imported into a DAW. Video is cut without re-encoding.

The analysis takes a couple of minutes and is saved to `cues.csv` and `sync.json`, so later runs skip it.

### 2. Check the song boundaries

`prepare` prints a table of the songs with their start and end times in the set video, and notes any songs that ran together without a gap. To fix a boundary, edit `songs/cues.csv` (times are `H:MM:SS.mmm` in the video named on that row), then re-cut just those songs:

```sh
./splitter show prepare -recut -songs 22,23 ../gate-city-09-11-26
```

### 3. Mix each song

Import the WAVs from a song's `channels/` folder into your DAW at the start of the session, mix, and bounce the mixdown into that song's `mix/` folder. WAV, AIFF, FLAC, M4A, and MP3 all work; if there are several files, the newest is used.

### 4. Build the song videos

```sh
./splitter show finish ../gate-city-09-11-26
```

For each song with a mixdown, `finish` matches the mixdown against the camera audio, so a bounce that starts a little early or late still lines up. It then writes `NN - Title.mp4` with the original video and the mix as AAC audio. Songs without a mixdown are listed and skipped, and finished songs are only rebuilt when their mixdown changes (or with `-force`).

### Show flags

| Flag | Command | Default | Description |
| :--- | :--- | :--- | :--- |
| `-audio` | prepare | `audio` | Folder with the board channel WAVs, inside the show folder. |
| `-video` | prepare | `video` | Folder with the set videos, inside the show folder. |
| `-setlist` | prepare | `setlist.md` or `setlist.txt` | Setlist file. |
| `-out` | both | `songs` | Folder for the song folders, inside the show folder. |
| `-songs` | both | all songs | Only process these song numbers, e.g. `3,7-9`. |
| `-pre` / `-post` | prepare | `2` / `5` | Seconds to keep before and after each song, never past the middle of the gap to the next song. |
| `-redetect` | prepare | `false` | Analyze the recordings again and overwrite `cues.csv`. |
| `-recut` | prepare | `false` | Cut channel WAVs and videos again even if they exist. |
| `-force` | finish | `false` | Rebuild song videos that are already up to date. |
| `-maxshift` | finish | `30` | Largest offset, in seconds, to search between a mixdown and its video. |

-----

## 🧪 How to Run Tests

To validate the configuration logic and segment calculation, run:

```sh
go test -v
```