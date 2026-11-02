package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type ApodResponse struct {
	Hdurl string `json:"hdurl"`
}

func main() {
	// 1. Prevent concurrent runs (mutex)
	const mutexName = "Global\\APOD_Wallpaper_Unique_Instance"
	mutex, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		fmt.Println("[ERROR] Mutex creation error:", err)
		return
	}
	defer syscall.CloseHandle(syscall.Handle(mutex))

	if windows.GetLastError() == syscall.EEXIST {
		fmt.Println("[INFO] Another instance is already running. Exiting.")
		return
	}

	// 2. Global execution timeout (60 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 3. Flags
	style := flag.String("style", "center", "Wallpaper style: center, tile, stretch")
	resetCache := flag.Bool("reset", false, "Force wallpaper cache reset before applying (causes screen flicker)")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Println("Downloads the latest APOD image from NASA and sets it as the desktop wallpaper.")
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  apod.exe              # Center style (recommended for 2+ monitors)")
		fmt.Println("  apod.exe -style=stretch  # Stretch (may show background bars on secondary monitors)")
		fmt.Println("  apod.exe -reset       # If the wallpaper doesn't update")
	}
	flag.Parse()

	// 4. Style validation
	wallpaperStyle, tileWallpaper := "0", "0" // Default is center
	switch strings.ToLower(*style) {
	case "center":
		wallpaperStyle, tileWallpaper = "0", "0"
	case "tile":
		wallpaperStyle, tileWallpaper = "0", "1"
	case "stretch":
		wallpaperStyle, tileWallpaper = "2", "0"
	default:
		fmt.Printf("[ERROR] Unknown style '%s'. Available: center, tile, stretch\n", *style)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// 5. Fetch JSON
	jsonURL := "https://science.nasa.gov/wp-json/wp/v2/apod-basic"
	fmt.Println("[INFO] Fetching JSON from:", jsonURL)
	
	req, _ := http.NewRequestWithContext(ctx, "GET", jsonURL, nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("[ERROR] Failed to fetch JSON:", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var apods []ApodResponse
	if err := json.Unmarshal(body, &apods); err != nil {
		fmt.Println("[ERROR] Failed to parse JSON:", err)
		return
	}
	if len(apods) == 0 || apods[0].Hdurl == "" {
		fmt.Println("[ERROR] hdurl not found in JSON response")
		return
	}
	
	imgUrl := apods[0].Hdurl
	fmt.Println("[INFO] Image URL:", imgUrl)

	// 6. Download image
	fmt.Println("[INFO] Downloading image...")
	imgReq, _ := http.NewRequestWithContext(ctx, "GET", imgUrl, nil)
	imgResp, err := client.Do(imgReq)
	if err != nil {
		fmt.Println("[ERROR] Failed to fetch image:", err)
		return
	}
	defer imgResp.Body.Close()

	contentType := imgResp.Header.Get("Content-Type")
	fmt.Println("[INFO] Content-Type:", contentType)

	// FIXED filename — overwritten every day
	imgPath := filepath.Join(os.TempDir(), "apod_today.jpg")
	
	out, err := os.Create(imgPath)
	if err != nil {
		fmt.Println("[ERROR] Failed to create file:", err)
		return
	}

	_, err = io.Copy(out, imgResp.Body)
	out.Close() // Important: close file before trying to set it as wallpaper
	if err != nil {
		fmt.Println("[ERROR] Failed to save image:", err)
		return
	}
	
	fileInfo, _ := os.Stat(imgPath)
	fmt.Printf("[INFO] Saved to: %s\n", imgPath)
	fmt.Printf("[INFO] File size: %d bytes\n", fileInfo.Size())

	if fileInfo.Size() < 1000 {
		fmt.Println("[ERROR] File is too small. Possible network error or invalid response.")
		return
	}

	// 7. Set wallpaper
	fmt.Println("[INFO] Setting wallpaper...")

	k, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Desktop`, registry.WRITE)
	if err != nil {
		fmt.Println("[ERROR] Registry open error:", err)
		return
	}
	
	if err := k.SetStringValue("Wallpaper", imgPath); err != nil {
		fmt.Println("[ERROR] Registry write error (Wallpaper):", err)
	}
	if err := k.SetStringValue("WallpaperStyle", wallpaperStyle); err != nil {
		fmt.Println("[ERROR] Registry write error (WallpaperStyle):", err)
	}
	if err := k.SetStringValue("TileWallpaper", tileWallpaper); err != nil {
		fmt.Println("[ERROR] Registry write error (TileWallpaper):", err)
	}
	k.Close()

	fmt.Printf("[INFO] Registry updated: WallpaperStyle=%s, TileWallpaper=%s\n", wallpaperStyle, tileWallpaper)

	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SystemParametersInfoW")
	wallpaperPathPtr := windows.StringToUTF16Ptr(imgPath)
	emptyPathPtr := windows.StringToUTF16Ptr("")

	// Optional cache reset (only if -reset flag is provided)
	if *resetCache {
		fmt.Println("[INFO] Forcing wallpaper cache reset...")
		proc.Call(20, 0, uintptr(unsafe.Pointer(emptyPathPtr)), 0x03)
		time.Sleep(150 * time.Millisecond) 
	}

	// Apply new wallpaper
	fmt.Println("[INFO] Applying new wallpaper...")
	ret, _, lastErr := proc.Call(
		20, // SPI_SETDESKWALLPAPER
		0,
		uintptr(unsafe.Pointer(wallpaperPathPtr)),
		0x03, // SPIF_UPDATEINIFILE | SPIF_SENDCHANGE
	)

	if ret == 0 {
		fmt.Println("[ERROR] SystemParametersInfoW failed. Error:", lastErr)
		return
	}

	fmt.Println("[SUCCESS] Done. Wallpaper style:", *style)
}