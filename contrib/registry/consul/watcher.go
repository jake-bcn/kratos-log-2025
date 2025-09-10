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

// getAllGoroutineStack 获取所有 goroutine 的完整堆栈
func getAllGoroutineStack() string {
	buf := make([]byte, 1024*1024) // 1MB 缓冲区
	for {
		n := runtime.Stack(buf, true) // true 表示获取所有 goroutine
		if n < len(buf) {
			return string(buf[:n])
		}
		// 如果缓冲区不够，加倍大小重试
		buf = make([]byte, 2*len(buf))
	}
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

// formatStack 优化堆栈输出格式
func formatStack(stack string) string {
	// 添加分隔线使输出更易读
	return "======================= FULL STACK TRACE =======================\n" +
		stack +
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

	// 获取所有 goroutine 的完整堆栈
	fullStack := getAllGoroutineStack()

	// 获取当前调用链（跳过3层）
	pc := make([]uintptr, 20)
	n := runtime.Callers(3, pc)
	callerTrace := ""
	if n > 0 {
		frames := runtime.CallersFrames(pc[:n])
		var trace strings.Builder
		for {
			frame, more := frames.Next()
			trace.WriteString(fmt.Sprintf("%s\n\t%s:%d\n",
				frame.Function, frame.File, frame.Line))
			if !more {
				break
			}
		}
		callerTrace = trace.String()
	}

	// 打印所有信息
	fmt.Printf(`
[%s] [Goroutine-%d] Watcher stopped
--- Current Caller Trace ---
%s
--- All Goroutines Stack Trace ---
%s
`, now, gid, callerTrace, formatStack(fullStack))

	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
		w.set.delete(w)
	}
	return nil
}
