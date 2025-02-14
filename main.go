package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	Models  []string
}

type ModelInfo struct {
	Name       string `json:"name"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
}

type TagsResponse struct {
	Models []ModelInfo `json:"models"`
}

var (
	mainLogger *log.Logger
	netLogger  *log.Logger
	version    = "dev"
	commit     = "none"
	buildDate  = "unknown"
)

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

	// 修改日志输出配置，确保网络日志同时输出到终端
	multiWriter := io.MultiWriter(logFile, os.Stdout)
	mainLogger = log.New(io.MultiWriter(logFile), "[MAIN] ", log.LstdFlags|log.Lshortfile) // 仅输出到文件
	netLogger = log.New(multiWriter, "[NET] ", log.LstdFlags|log.Lshortfile)               // 同时输出到文件和终端

	return logFile
}

func checkAPI(ctx context.Context, url string) *APIStatus {
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url+"/api/tags", nil)
	if err != nil {
		mainLogger.Printf("创建请求失败: %s (%v)", url, err)
		return &APIStatus{URL: url, Healthy: false, Error: err.Error()}
	}

	// 添加重试机制
	var resp *http.Response
	for retry := 0; retry < 3; retry++ {
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(retry+1) * 500 * time.Millisecond)
	}

	// 优化响应体处理
	defer func() {
		if resp != nil && resp.Body != nil {
			io.Copy(io.Discard, resp.Body) // 确保连接可重用
			resp.Body.Close()
		}
	}()

	if err != nil {
		mainLogger.Printf("请求失败: %s (%v)", url, err)
		return &APIStatus{URL: url, Healthy: false, Error: err.Error()}
	}

	if resp.StatusCode != http.StatusOK {
		netLogger.Printf("非200响应: %s (%d)", url, resp.StatusCode)
		return &APIStatus{URL: url, Healthy: false, Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	// 新增模型名称解析
	var tagsResp TagsResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		netLogger.Printf("读取响应失败: %s (%v)", url, err)
		return &APIStatus{URL: url, Healthy: false, Error: "读取响应体失败"}
	}

	if err := json.Unmarshal(body, &tagsResp); err != nil {
		netLogger.Printf("解析JSON失败: %s (%v)", url, err)
		return &APIStatus{URL: url, Healthy: false, Error: "无效的响应格式"}
	}

	// 提取模型名称
	models := make([]string, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		models = append(models, m.Name)
	}

	return &APIStatus{
		URL:     url,
		Healthy: true,
		Models:  models,
	}
}

func worker(ctx context.Context, wg *sync.WaitGroup, jobs <-chan string, results chan<- *APIStatus) {
	defer wg.Done()
	for url := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
			results <- checkAPI(ctx, url)
		}
	}
}

// 修改导出CSV函数
func exportToCSV(results []*APIStatus) {
	if len(results) == 0 {
		return
	}

	filename := fmt.Sprintf("results_%s.csv", time.Now().Format("20060102-150405"))
	file, err := os.Create(filename)
	if err != nil {
		mainLogger.Printf("创建CSV文件失败: %v", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写入CSV头（保持不变）
	header := []string{"URL", "Model"}
	if err := writer.Write(header); err != nil {
		mainLogger.Printf("写入CSV头失败: %v", err)
		return
	}

	// 修改数据写入方式
	for _, status := range results {
		if status.Healthy {
			var modelStr string
			if len(status.Models) == 0 {
				modelStr = "无模型"
			} else {
				modelStr = strings.Join(status.Models, "; ") // 用分号分隔多个模型
			}
			record := []string{status.URL, modelStr}
			if err := writer.Write(record); err != nil {
				mainLogger.Printf("写入记录失败: %v", err)
			}
		}
	}

	mainLogger.Printf("结果已导出到: %s", filename)
	fmt.Printf("\n检测结果已保存到: \033[34m%s\033[0m\n", filename)
}

func main() {
	// 添加版本信息输出
	fmt.Printf("Ollama检测器 v%s (构建于 %s)\n", version, buildDate)

	// 初始化配置
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

	// 修改context创建方式（移除超时设置）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 动态调整 worker 数量
	workerCount := calculateWorkerCount(len(urls))
	jobs := make(chan string, len(urls))
	results := make(chan *APIStatus, len(urls))

	// 启动 worker 池
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker(ctx, &wg, jobs, results)
	}

	var outputWG sync.WaitGroup
	outputWG.Add(1)

	// 新增结果收集切片
	var successfulResults []*APIStatus

	go func() {
		defer outputWG.Done()
		var counter int

		for result := range results {
			fmt.Print("\033[2K\r")
			if result.Healthy {
				fmt.Printf("\033[32m✓\033[0m %s\n", result.URL)
				fmt.Printf("可用模型: %v\n", result.Models)
				successfulResults = append(successfulResults, result)
			} else {
				fmt.Printf("\033[31m✗\033[0m %s\n", result.Error)
			}

			counter++
			fmt.Printf("\r\033[33m已处理: %d/%d\033[0m",
				counter,
				len(urls))
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

	// 导出CSV
	exportToCSV(successfulResults)

	// 修改最后的统计日志
	defer func() {
		mainLogger.Printf("检测完成: 成功%d/失败%d",
			len(successfulResults),
			len(urls)-len(successfulResults))
	}()
}

func calculateWorkerCount(taskNum int) int {
	const maxWorkers = 20
	const minWorkers = 3
	workers := taskNum / 2
	if workers < minWorkers {
		return minWorkers
	}
	if workers > maxWorkers {
		return maxWorkers
	}
	return workers
}
