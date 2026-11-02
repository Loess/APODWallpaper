# APOD Wallpaper for Windows 7

A lightweight Go utility that fetches the daily Astronomy Picture of the Day (APOD) from NASA and sets it as your Windows 7 desktop wallpaper.

## Why this exists
Ken Jackson's classic *APODWallpaper* stopped working in October 2026 because NASA retired the old APOD API. This tool uses the new active endpoint (`science.nasa.gov/wp-json/wp/v2/apod-basic`).

For a list of national and language-specific APOD mirrors, see:  
https://apod.com/en/lib/about_apod.html

## Requirements
- Windows 7 (x64)
- Go **1.20.x strictly** (Go 1.21+ dropped Windows 7 support).  
  Download the last compatible version here: https://go.dev/dl/?old#go1.20 (e.g., `go1.20.14.windows-amd64.msi`)

## Build
```cmd
go mod init apod
go get golang.org/x/sys/windows
go build -ldflags="-s -w" -o apod.exe apod.go
```

## Usage
Run directly to see console output (useful for debugging):

    apod.exe

**Available Flags:**
- `-style=fit` or `-style=scaled` *(default)*: Scales image to fit screen while keeping aspect ratio (recommended for all setups).
- `-style=center`: Displays image at original size, centered.
- `-style=stretch`: Stretches the image to fill the screen (may distort or show background bars on multi-monitor).
- `-style=tile`: Tiles the image across the desktop.
- `-reset`: Forces a wallpaper cache reset before applying (use only if the image doesn't update).

## Run Silently (No Console Window)
The repository includes `apod_hidden.vbs`, **just copy it to the same folder as `apod.exe`** — no edits needed.

The script automatically detects its own location and runs `apod.exe` from the same directory, so you can move both apod.exe and apod_hidden.vbs anywhere.

Double-clicking `apod_hidden.vbs` will execute the program completely hidden.

## Windows 7 Task Scheduler Setup
To update the wallpaper automatically every day:

1. Open Task Scheduler (`taskschd.msc`).
2. Click **Create Basic Task...** on the right panel.
3. Name it `NASA APOD Wallpaper`, set the trigger to **Daily** at your preferred time.
4. Action: **Start a program**.
5. Program/script: `C:\Windows\System32\wscript.exe`
6. Add arguments: `"C:\path\to\apod_hidden.vbs"` *(keep the quotes)*.
7. Click **Finish**.
8. **Crucial Step:** Right-click the new task in the list > **Properties**.
   - **General tab:** Select **Run only when user is logged on** (required to change the user's wallpaper) and check **Run with highest privileges**.
   - **Conditions tab:** Uncheck "Start the task only if the computer is on AC power" (if using a laptop).
   - **Settings tab:** Check **Run task as soon as possible after a scheduled start is missed**. This ensures the wallpaper updates even if the computer was off or asleep at the scheduled time (it will run once after boot, not multiple times for each missed run).

## Troubleshooting
- If the wallpaper doesn't change, run `apod.exe -reset` manually to clear the Win7 cache.
- Check `%TEMP%\apod_today.jpg`. If the file size is under 1KB, the NASA API likely returned an error or invalid data.
