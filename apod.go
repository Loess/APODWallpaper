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
	// 1. Защита от повторного запуска (мьютекс)
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

	// 2. Общий таймаут на выполнение (60 секунд)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 3. Флаги
	style := flag.String("style", "center", "Стиль обоев: center (по центру), tile (замостить), stretch (растянуть)")
	resetCache := flag.Bool("reset", false, "Принудительно сбросить кэш обоев перед установкой (вызывает мерцание)")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Использование: %s [опции]\n\n", os.Args[0])
		fmt.Println("Скачивает свежее изображение APOD от NASA и устанавливает как обои.")
		fmt.Println("\nОпции:")
		flag.PrintDefaults()
		fmt.Println("\nПримеры:")
		fmt.Println("  apod.exe              # Установка по центру (рекомендуется для 2+ мониторов)")
		fmt.Println("  apod.exe -style=stretch  # Растянуть (на 2 мониторах может дать поля)")
		fmt.Println("  apod.exe -reset       # Если обои не обновляются")
	}
	flag.Parse()

	// 4. Валидация стиля
	wallpaperStyle, tileWallpaper := "0", "0" // По умолчанию center
	switch strings.ToLower(*style) {
	case "center":
		wallpaperStyle, tileWallpaper = "0", "0"
	case "tile":
		wallpaperStyle, tileWallpaper = "0", "1"
	case "stretch":
		wallpaperStyle, tileWallpaper = "2", "0"
	default:
		fmt.Printf("[ERROR] Неизвестный стиль '%s'. Доступны: center, tile, stretch\n", *style)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// 5. Загрузка JSON
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

	// 6. Загрузка изображения
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

	// ФИКСИРОВАННОЕ имя файла — перезаписываем каждый день
	imgPath := filepath.Join(os.TempDir(), "apod_today.jpg")
	
	out, err := os.Create(imgPath)
	if err != nil {
		fmt.Println("[ERROR] Failed to create file:", err)
		return
	}

	_, err = io.Copy(out, imgResp.Body)
	out.Close() // Важно закрыть файл до попытки установить его как обои
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

	// 7. Установка обоев
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

	// Опциональный сброс кэша (только если передан флаг -reset)
	if *resetCache {
		fmt.Println("[INFO] Forcing wallpaper cache reset...")
		proc.Call(20, 0, uintptr(unsafe.Pointer(emptyPathPtr)), 0x03)
		time.Sleep(150 * time.Millisecond) 
	}

	// Применяем новые обои
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