package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type HealthResponse struct {
	Models []struct {
		Name       string    `json:"name"`
		Digest     string    `json:"digest"`
		ModifiedAt time.Time `json:"modified_at"`
	} `json:"models"`
}

type APIStatus struct {
	URL     string
	Healthy bool
	Error   string
}

var (
	mainLogger *log.Logger // 主日志记录器
	netLogger  *log.Logger // 网络日志记录器
)

var failures atomic.Int32

func initLogging() *os.File {
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("创建日志目录失败: %v", err)
	}

	logPath := filepath.Join(logDir, time.Now().Format("20060102-150405.log"))
	logFile, err := os.Create(logPath)
	if err != nil {
		log.Fatalf("创建日志文件失败: %v", err)
	}

	// 主日志（错误和成功）：仅写入文件
	mainLogger = log.New(logFile, "", log.LstdFlags|log.Lshortfile)

	// 网络日志：单独记录
	netLogger = log.New(logFile, "", log.LstdFlags|log.Lshortfile)

	// 终端输出单独处理
	log.SetOutput(os.Stdout) // 标准log包用于进度显示

	return logFile
}

func checkAPI(ctx context.Context, url string) *APIStatus {
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url+"/api/tags", nil)
	if err != nil {
		mainLogger.Printf("[ERROR] 创建请求失败: %s (%v)", url, err)
		return &APIStatus{URL: url, Healthy: false, Error: err.Error()}
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Ollama-Checker/2.2")

	// 修改网络日志记录方式
	reqDump, _ := httputil.DumpRequestOut(req, false)
	netLogger.Printf("[NET] 请求 → %s\n%s", url, string(reqDump))

	resp, err := client.Do(req)
	if err != nil {
		mainLogger.Printf("[ERROR] 网络错误: %s (%v)", url, err)
		return &APIStatus{URL: url, Healthy: false, Error: err.Error()}
	}
	defer resp.Body.Close()

	respDump, _ := httputil.DumpResponse(resp, false)
	netLogger.Printf("[NET] 响应 ← %s\n%s", url, string(respDump))

	// 记录响应体（最多1KB）
	var bodyBuffer bytes.Buffer
	teeReader := io.TeeReader(resp.Body, &bodyBuffer)
	body, _ := io.ReadAll(io.LimitReader(teeReader, 1024))
	resp.Body = io.NopCloser(teeReader)

	if resp.StatusCode != http.StatusOK {
		mainLogger.Printf("[ERROR] 异常状态码: %s (%d)\n响应体: %s",
			url, resp.StatusCode, string(body))
		return &APIStatus{URL: url, Healthy: false,
			Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))}
	}

	var health HealthResponse
	if err := json.NewDecoder(io.MultiReader(bytes.NewReader(body), resp.Body)).Decode(&health); err != nil {
		mainLogger.Printf("[ERROR] JSON解析失败: %s\n原始响应: %s", url, string(body))
		return &APIStatus{URL: url, Healthy: false, Error: "invalid JSON: " + err.Error()}
	}

	if len(health.Models) == 0 {
		return &APIStatus{URL: url, Healthy: false, Error: "no available models"}
	}

	hasValidModel := false
	for _, model := range health.Models {
		if model.Name != "" && model.Digest != "" {
			hasValidModel = true
			break
		}
	}
	if !hasValidModel {
		return &APIStatus{URL: url, Healthy: false, Error: "no valid models"}
	}

	mainLogger.Printf("[SUCCESS] 有效节点: %s (模型数: %d)", url, len(health.Models))
	return &APIStatus{URL: url, Healthy: true}
}

func worker(ctx context.Context, wg *sync.WaitGroup, jobs <-chan string, results chan<- *APIStatus) {
	defer wg.Done()
	for url := range jobs {
		result := checkAPI(ctx, url)
		results <- result
		fmt.Printf("\r\033[33m处理中: %d/%d\033[0m", len(results), cap(results))
	}
}

func main() {
	// 初始化日志
	logFile := initLogging()
	defer logFile.Close()

	filePath := flag.String("file", "", "包含URL列表的文件路径")
	flag.Parse()

	var urls []string
	if *filePath != "" {
		content, err := os.ReadFile(*filePath)
		if err != nil {
			fmt.Printf("无法读取文件: %v\n", err)
			os.Exit(1)
		}
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				urls = append(urls, line)
			}
		}
	} else {
		urls = flag.Args()
	}

	if len(urls) == 0 {
		fmt.Println("请通过-file指定文件或直接提供API地址")
		os.Exit(1)
	}

	// 在main函数开始时添加版本信息
	mainLogger.Printf("Ollama检测器启动 v2.2")
	mainLogger.Printf("参数: %v", os.Args)
	mainLogger.Printf("任务总数: %d", len(urls))

	// 在main函数顶部添加失败计数器
	var failures atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	jobs := make(chan string, len(urls))
	results := make(chan *APIStatus, len(urls))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go worker(ctx, &wg, jobs, results)
	}

	var outputWG sync.WaitGroup
	outputWG.Add(1)
	go func() {
		defer outputWG.Done()
		var counter int

		for result := range results {
			fmt.Print("\033[2K\r")
			if result.Healthy {
				fmt.Printf("\033[32m✓\033[0m %s\n", result.URL)
			} else {
				failures.Add(1) // 使用原子计数器
			}

			counter++
			fmt.Printf("\r\033[33m已处理: %d/%d | 失败: %d\033[0m",
				counter,
				len(urls),
				failures.Load())
		}
	}()

	for _, url := range urls {
		jobs <- url
	}
	close(jobs)

	wg.Wait()
	close(results)

	outputWG.Wait()
	fmt.Println("\n检测完成")

	// 修改最后的统计日志
	defer func() {
		mainLogger.Printf("检测完成: 成功%d/失败%d",
			len(urls)-int(failures.Load()),
			failures.Load())
	}()
}
