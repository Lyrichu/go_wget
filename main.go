package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultOutputFilename = "index.html"
)

var (
	outputFile = flag.String("o", "", "输出文件名")
	headers    = flag.String("h", "", "自定义请求头")
	verbose    = flag.Bool("v", true, "显示详细信息")
)

type Chunk struct {
	Start int64
	End   int64
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法: gowget [选项] <URL>\n\n选项:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n示例:\n  gowget https://example.com\n  gowget -o file.zip https://example.com/file.zip\n")
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	url := flag.Arg(0)
	err := downloadFile(url, *outputFile, *headers, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	if *verbose {
		fmt.Println("\n下载完成")
	}
}

func downloadFile(urlLink, filename, headers string, verbose bool) error {
	parsedURL, err := url.ParseRequestURI(urlLink)
	if err != nil {
		return fmt.Errorf("无效URL: %v", err)
	}

	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS12,
				CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
			},
		},
	}

	// 获取文件名
	if filename == "" {
		filename = getFilename(parsedURL)
	}

	// 检查是否支持分块下载
	supportRange, fileSize, err := checkRangeSupport(client, urlLink)
	if err != nil {
		return err
	}
	fmt.Printf("支持分块下载:%v\n", supportRange)

	if !supportRange {
		return sequentialDownload(client, parsedURL, filename, headers, verbose)
	}

	// 创建临时文件
	tmpFile := filename + ".tmp"
	fd, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %v", err)
	}
	defer func() {
		fd.Close()
		os.Remove(tmpFile)
	}()

	// 启动并发下载
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		wg         sync.WaitGroup
		errChan    = make(chan error, 1)
		downloaded int64
	)

	// 计算分块
	concurrency := calculateConcurrency(fileSize)
	chunks := make([]Chunk, concurrency)
	chunkSize := fileSize / int64(concurrency)

	for i := 0; i < concurrency; i++ {
		chunks[i].Start = int64(i) * chunkSize
		chunks[i].End = chunks[i].Start + chunkSize - 1
	}
	chunks[concurrency-1].End = fileSize - 1 // 最后一块包含剩余字节

	// 进度显示
	if verbose {
		go showProgress(ctx, fileSize, &downloaded)
	}

	// 启动下载协程
	for i, chunk := range chunks {
		wg.Add(1)
		go func(c Chunk, idx int) {
			defer wg.Done()
			if err := downloadChunk(client, urlLink, c, fd, headers, &downloaded); err != nil {
				select {
				case errChan <- fmt.Errorf("分块%d错误: %v", idx, err):
				default:
				}
				cancel()
			}
		}(chunk, i)
	}

	wg.Wait()
	close(errChan)

	if err := <-errChan; err != nil {
		return err
	}

	// 重命名临时文件
	if err := os.Rename(tmpFile, filename); err != nil {
		return fmt.Errorf("重命名文件失败: %v", err)
	}

	return nil
}

func checkRangeSupport(client http.Client, url string) (bool, int64, error) {
	req, _ := http.NewRequest("HEAD", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, fmt.Errorf("HEAD请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, 0, fmt.Errorf("服务器返回状态码: %d", resp.StatusCode)
	}

	return resp.Header.Get("Accept-Ranges") == "bytes", resp.ContentLength, nil
}

func calculateConcurrency(fileSize int64) int {
	if fileSize > 1024*1024*1024 { // 1GB以上
		return 8
	}
	return 4
}

func downloadChunk(client http.Client, url string, chunk Chunk, file *os.File, headers string, downloaded *int64) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.Start, chunk.End))

	// 添加自定义请求头
	if headers != "" {
		for _, pair := range strings.Split(headers, ",") {
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) == 2 {
				req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("无效的分块响应状态码: %d", resp.StatusCode)
	}

	buf := make([]byte, 32*1024) // 32KB缓冲区
	current := chunk.Start

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := file.WriteAt(buf[:n], current)
			if writeErr != nil {
				return writeErr
			}
			atomic.AddInt64(downloaded, int64(n))
			current += int64(n)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return nil
}

func showProgress(ctx context.Context, total int64, downloaded *int64) {
	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d := atomic.LoadInt64(downloaded)
			elapsed := time.Since(start).Seconds()
			speed := float64(d) / elapsed

			fmt.Printf("\r%s / %s | %.2f MB/s",
				formatSize(d),
				formatSize(total),
				speed/(1024*1024))

		case <-ctx.Done():
			fmt.Println()
			return
		}
	}
}

func sequentialDownload(client http.Client, parsedURL *url.URL, filename, headers string, verbose bool) error {
	req, err := http.NewRequest("GET", parsedURL.String(), nil)
	if err != nil {
		return err
	}

	// 添加自定义请求头
	if headers != "" {
		for _, pair := range strings.Split(headers, ",") {
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) == 2 {
				req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回状态码: %d", resp.StatusCode)
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	var downloaded int64
	start := time.Now()

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, err = file.Write(buf[:n])
			if err != nil {
				return err
			}
			downloaded += int64(n)

			if verbose {
				elapsed := time.Since(start).Seconds()
				speed := float64(downloaded) / elapsed
				fmt.Printf("\r%s | %.2f MB/s",
					formatSize(downloaded),
					speed/(1024*1024))
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return nil
}

func getFilename(u *url.URL) string {
	base := filepath.Base(u.Path)
	if base == "." || base == "/" {
		return defaultOutputFilename
	}
	return base
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
