package consul

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/registry"
)

type watcher struct {
	event chan struct{}
	set   *serviceSet

	// for cancel
	ctx    context.Context
	cancel context.CancelFunc
}

// getAncestorChain 获取当前 goroutine 的祖先链
func getAncestorChain() string {
	currentGID := getCurrentGoroutineID()

	// 获取所有 goroutine 的堆栈
	buf := make([]byte, 1024*1024) // 1MB 缓冲区
	n := runtime.Stack(buf, true)
	allStacks := string(buf[:n])

	// 构建 goroutine 创建关系图
	creationGraph := buildCreationGraph(allStacks)

	// 追踪祖先链
	chain := traceAncestry(currentGID, creationGraph)

	// 提取祖先链的堆栈信息
	return extractChainStacks(chain, allStacks)
}

// buildCreationGraph 构建 goroutine 创建关系图
func buildCreationGraph(allStacks string) map[uint64]uint64 {
	graph := make(map[uint64]uint64)
	stacks := strings.Split(allStacks, "\n\n")

	for _, stack := range stacks {
		if stack == "" {
			continue
		}

		// 提取当前 goroutine ID
		lines := strings.Split(stack, "\n")
		if len(lines) < 1 {
			continue
		}

		currentID, err := extractGoroutineID(lines[0])
		if err != nil {
			continue
		}

		// 查找创建者
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.Contains(lines[i], "created by") {
				creatorID, err := extractCreatorID(lines[i])
				if err == nil {
					graph[currentID] = creatorID
				}
				break
			}
		}
	}

	return graph
}

// traceAncestry 追踪祖先链
func traceAncestry(currentGID uint64, graph map[uint64]uint64) []uint64 {
	chain := []uint64{currentGID}

	for {
		creator, exists := graph[currentGID]
		if !exists {
			break
		}

		// 避免循环引用
		if contains(chain, creator) {
			break
		}

		chain = append(chain, creator)
		currentGID = creator
	}

	return chain
}

// extractChainStacks 提取祖先链的堆栈信息
func extractChainStacks(chain []uint64, allStacks string) string {
	var builder strings.Builder
	stacks := strings.Split(allStacks, "\n\n")

	for _, gid := range chain {
		for _, stack := range stacks {
			if stack == "" {
				continue
			}

			lines := strings.Split(stack, "\n")
			if len(lines) < 1 {
				continue
			}

			id, err := extractGoroutineID(lines[0])
			if err != nil || id != gid {
				continue
			}

			builder.WriteString(stack)
			builder.WriteString("\n\n")
			break
		}
	}

	if builder.Len() == 0 {
		return "No ancestor chain found"
	}

	return builder.String()
}

// extractGoroutineID 从堆栈行提取 goroutine ID
func extractGoroutineID(line string) (uint64, error) {
	if !strings.HasPrefix(line, "goroutine ") {
		return 0, fmt.Errorf("invalid goroutine line")
	}

	parts := strings.Split(line, " ")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid goroutine line format")
	}

	idStr := parts[1]
	return strconv.ParseUint(idStr, 10, 64)
}

// extractCreatorID 从创建行提取创建者 ID
func extractCreatorID(line string) (uint64, error) {
	if !strings.Contains(line, "in goroutine ") {
		return 0, fmt.Errorf("no creator info")
	}

	parts := strings.Split(line, "in goroutine ")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid creator format")
	}

	idStr := strings.TrimSpace(parts[1])
	return strconv.ParseUint(idStr, 10, 64)
}

// contains 检查切片是否包含元素
func contains(slice []uint64, item uint64) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// getCurrentGoroutineID 获取当前 goroutine ID
func getCurrentGoroutineID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	s := strings.TrimPrefix(string(b), "goroutine ")
	s = s[:strings.Index(s, " ")]
	id, _ := strconv.ParseUint(s, 10, 64)
	return id
}

// formatAncestorChain 格式化祖先链输出
func formatAncestorChain(chain string) string {
	if chain == "No ancestor chain found" {
		return chain
	}

	return "======================= ANCESTOR CHAIN =======================\n" +
		chain +
		"\n==========================================================="
}

func (w *watcher) Next() (services []*registry.ServiceInstance, err error) {
	if err = w.ctx.Err(); err != nil {
		return
	}

	select {
	case <-w.ctx.Done():
		err = w.ctx.Err()
		return
	case <-w.event:
	}

	ss, ok := w.set.services.Load().([]*registry.ServiceInstance)
	if ok {
		services = append(services, ss...)
	}
	return
}

func (w *watcher) Stop() error {
	now := time.Now().Format(time.RFC3339)
	gid := getCurrentGoroutineID()

	// 获取祖先链
	ancestorChain := getAncestorChain()

	// 打印信息
	fmt.Printf(`
[%s] [Goroutine-%d] Watcher stopped
--- Ancestor Chain Stack Trace ---
%s
`, now, gid, formatAncestorChain(ancestorChain))

	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
		w.set.delete(w)
	}
	return nil
}
