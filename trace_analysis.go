package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// TraceAnalyzer 分析trace数据的访问模式
type TraceAnalyzer struct {
	requests     []Request
	hashIDFreq   map[int]int     // hash_id的全局频率
	hashIDFirst  map[int]int     // 每个hash_id首次出现的请求序号
	hashIDLast   map[int]int     // 每个hash_id最后出现的请求序号
	prefixFreq   map[string]int  // 前缀模式的频率
}

func NewTraceAnalyzer() *TraceAnalyzer {
	return &TraceAnalyzer{
		hashIDFreq:  make(map[int]int),
		hashIDFirst: make(map[int]int),
		hashIDLast:  make(map[int]int),
		prefixFreq:  make(map[string]int),
	}
}

func (ta *TraceAnalyzer) LoadAndAnalyze(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	requestIndex := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var request Request
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			fmt.Printf("解析请求失败: %v\n", err)
			continue
		}

		ta.requests = append(ta.requests, request)
		ta.analyzeRequest(request, requestIndex)
		requestIndex++
	}

	return scanner.Err()
}

func (ta *TraceAnalyzer) analyzeRequest(request Request, index int) {
	// 分析每个hash_id的访问模式
	for _, hashID := range request.HashIDs {
		ta.hashIDFreq[hashID]++

		if _, exists := ta.hashIDFirst[hashID]; !exists {
			ta.hashIDFirst[hashID] = index
		}
		ta.hashIDLast[hashID] = index
	}

	// 分析前缀模式
	for i := 1; i <= len(request.HashIDs) && i <= 5; i++ {
		prefix := fmt.Sprintf("%v", request.HashIDs[:i])
		ta.prefixFreq[prefix]++
	}
}

func (ta *TraceAnalyzer) PrintAnalysis() {
	fmt.Println("\n============= Trace数据访问模式分析 =============")
	fmt.Printf("总请求数: %d\n", len(ta.requests))
	fmt.Printf("唯一hash_id数: %d\n", len(ta.hashIDFreq))

	// 找出最频繁的hash_id
	fmt.Println("\n--- 最频繁的hash_ids ---")
	type hashFreq struct {
		hashID int
		freq   int
	}

	var freqList []hashFreq
	for hashID, freq := range ta.hashIDFreq {
		freqList = append(freqList, hashFreq{hashID, freq})
	}

	// 简单排序（冒泡排序）
	for i := 0; i < len(freqList); i++ {
		for j := i + 1; j < len(freqList); j++ {
			if freqList[j].freq > freqList[i].freq {
				freqList[i], freqList[j] = freqList[j], freqList[i]
			}
		}
	}

	// 显示前20个最频繁的
	for i := 0; i < 20 && i < len(freqList); i++ {
		hashID := freqList[i].hashID
		freq := freqList[i].freq
		firstReq := ta.hashIDFirst[hashID]
		lastReq := ta.hashIDLast[hashID]
		span := lastReq - firstReq
		fmt.Printf("hash_id=%d: 频率=%d, 首次=#%d, 最后=#%d, 跨度=%d\n",
			hashID, freq, firstReq, lastReq, span)
	}

	// 分析时间局部性
	fmt.Println("\n--- 时间局部性分析 ---")
	ta.analyzeTemporalLocality()

	// 分析空间局部性
	fmt.Println("\n--- 空间局部性分析 ---")
	ta.analyzeSpatialLocality()

	// 分析为什么LFU可能优于LRU
	fmt.Println("\n--- LFU vs LRU 性能分析 ---")
	ta.analyzeLFUvsLRU()
}

func (ta *TraceAnalyzer) analyzeTemporalLocality() {
	// 计算每个hash_id的重访间隔
	lastSeen := make(map[int]int)
	intervals := make([]int, 0)

	for reqIdx, request := range ta.requests {
		for _, hashID := range request.HashIDs {
			if lastIdx, exists := lastSeen[hashID]; exists {
				interval := reqIdx - lastIdx
				intervals = append(intervals, interval)
			}
			lastSeen[hashID] = reqIdx
		}
	}

	if len(intervals) == 0 {
		fmt.Println("无重访数据")
		return
	}

	// 计算重访间隔统计
	shortIntervals := 0  // <= 10
	mediumIntervals := 0 // 11-100
	longIntervals := 0   // > 100

	for _, interval := range intervals {
		if interval <= 10 {
			shortIntervals++
		} else if interval <= 100 {
			mediumIntervals++
		} else {
			longIntervals++
		}
	}

	totalIntervals := len(intervals)
	fmt.Printf("重访间隔统计 (总重访次数: %d):\n", totalIntervals)
	fmt.Printf("  短间隔 (≤10): %d (%.1f%%)\n", shortIntervals, float64(shortIntervals)*100/float64(totalIntervals))
	fmt.Printf("  中等间隔 (11-100): %d (%.1f%%)\n", mediumIntervals, float64(mediumIntervals)*100/float64(totalIntervals))
	fmt.Printf("  长间隔 (>100): %d (%.1f%%)\n", longIntervals, float64(longIntervals)*100/float64(totalIntervals))
}

func (ta *TraceAnalyzer) analyzeSpatialLocality() {
	// 分析相邻hash_id的访问模式
	consecutiveCount := 0
	totalPairs := 0

	for _, request := range ta.requests {
		for i := 0; i < len(request.HashIDs)-1; i++ {
			if request.HashIDs[i+1] == request.HashIDs[i]+1 {
				consecutiveCount++
			}
			totalPairs++
		}
	}

	if totalPairs > 0 {
		fmt.Printf("连续hash_id比例: %.1f%% (%d/%d)\n",
			float64(consecutiveCount)*100/float64(totalPairs), consecutiveCount, totalPairs)
	}

	// 分析请求长度分布
	lengthDist := make(map[int]int)
	for _, request := range ta.requests {
		lengthDist[len(request.HashIDs)]++
	}

	fmt.Println("请求长度分布:")
	for length := 1; length <= 20; length++ {
		if count, exists := lengthDist[length]; exists {
			fmt.Printf("  长度%d: %d次 (%.1f%%)\n", length, count, float64(count)*100/float64(len(ta.requests)))
		}
	}
}

func (ta *TraceAnalyzer) analyzeLFUvsLRU() {
	// 分析"冷启动"阶段vs"稳定运行"阶段
	totalRequests := len(ta.requests)
	coldStartEnd := totalRequests / 4  // 前25%为冷启动阶段

	coldStartAccess := make(map[int]int)
	steadyStateAccess := make(map[int]int)

	for i, request := range ta.requests {
		targetMap := steadyStateAccess
		if i < coldStartEnd {
			targetMap = coldStartAccess
		}

		for _, hashID := range request.HashIDs {
			targetMap[hashID]++
		}
	}

	fmt.Printf("阶段分析 (冷启动: 前%d请求, 稳定期: 后%d请求):\n", coldStartEnd, totalRequests-coldStartEnd)

	// 找出在冷启动阶段就高频的hash_id
	coldHotIDs := 0
	steadyNewIDs := 0
	bothPhaseIDs := 0

	for hashID := range ta.hashIDFreq {
		inCold := coldStartAccess[hashID] > 5   // 冷启动期被访问>5次
		inSteady := steadyStateAccess[hashID] > 5 // 稳定期被访问>5次

		if inCold && inSteady {
			bothPhaseIDs++
		} else if inCold {
			coldHotIDs++
		} else if inSteady {
			steadyNewIDs++
		}
	}

	fmt.Printf("  两阶段都活跃的hash_id: %d\n", bothPhaseIDs)
	fmt.Printf("  仅冷启动活跃: %d\n", coldHotIDs)
	fmt.Printf("  仅稳定期活跃: %d\n", steadyNewIDs)

	// 这可能解释了LFU的优势：
	// 如果有很多hash_id在整个trace期间都保持高频访问，
	// LFU会给它们提供持续保护，而LRU可能会因为临时的其他访问而错误淘汰它们
	fmt.Printf("\n💡 LFU可能优于LRU的原因分析:\n")
	if bothPhaseIDs > steadyNewIDs {
		fmt.Printf("  - 发现%d个长期高频hash_id，它们贯穿整个trace\n", bothPhaseIDs)
		fmt.Printf("  - LFU能持续保护这些'全局热点'，即使临时被其他数据'挤出'LRU序列\n")
		fmt.Printf("  - LRU在面对突发访问时可能错误淘汰这些长期有价值的blocks\n")
	}

	shortIntervals := ta.countShortIntervals()
	totalIntervals := ta.countTotalIntervals()
	if totalIntervals > 0 && float64(shortIntervals)*100/float64(totalIntervals) < 30 {
		fmt.Printf("  - 重访间隔较长，说明'最近使用'不是好的预测指标\n")
		fmt.Printf("  - '访问频率'比'最近访问时间'更能预测未来的访问概率\n")
	}
}

func (ta *TraceAnalyzer) countShortIntervals() int {
	lastSeen := make(map[int]int)
	shortIntervals := 0

	for reqIdx, request := range ta.requests {
		for _, hashID := range request.HashIDs {
			if lastIdx, exists := lastSeen[hashID]; exists {
				if reqIdx-lastIdx <= 10 {
					shortIntervals++
				}
			}
			lastSeen[hashID] = reqIdx
		}
	}
	return shortIntervals
}

func (ta *TraceAnalyzer) countTotalIntervals() int {
	lastSeen := make(map[int]int)
	totalIntervals := 0

	for reqIdx, request := range ta.requests {
		for _, hashID := range request.HashIDs {
			if _, exists := lastSeen[hashID]; exists {
				totalIntervals++
			}
			lastSeen[hashID] = reqIdx
		}
	}
	return totalIntervals
}

// RunTraceAnalysis 运行trace分析
func RunTraceAnalysis() {
	fmt.Println("开始分析trace数据访问模式...")

	analyzer := NewTraceAnalyzer()
	if err := analyzer.LoadAndAnalyze("mooncake_trace.jsonl"); err != nil {
		fmt.Printf("加载trace数据失败: %v\n", err)
		return
	}

	analyzer.PrintAnalysis()
}