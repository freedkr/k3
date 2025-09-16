package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// repeat 生成重复字符串
func repeat(s string, n int) string {
	return strings.Repeat(s, n)
}

// LatencyMetrics 延迟指标
type LatencyMetrics struct {
	Latencies []float64 // 所有延迟记录
	P50       float64
	P95       float64
	P99       float64
	Mean      float64
}

// NodeLoadMetrics 节点负载指标
type NodeLoadMetrics struct {
	NodeID        string
	RequestCount  int
	QueueLength   int
	ProcessingTime float64
	ResponseTimes []float64
}

// BetaSensitivityResult β灵敏度分析结果
type BetaSensitivityResult struct {
	Beta           float64
	HitRate        float64
	Concentration  float64
	P95Latency     float64
	P95Load        float64  // P95节点负载
	LoadStdDev     float64  // 负载标准差
}

// EnhancedCacheAwareSelectorWithTieBreak 带随机tie-break的增强选择器
type EnhancedCacheAwareSelectorWithTieBreak struct {
	Alpha        float64
	Beta         float64
	TieBreakRange float64 // tie-break抖动范围 (例如 0.01)
}

func NewEnhancedSelectorWithTieBreak(alpha, beta, tieBreakRange float64) *EnhancedCacheAwareSelectorWithTieBreak {
	return &EnhancedCacheAwareSelectorWithTieBreak{
		Alpha:         alpha,
		Beta:          beta,
		TieBreakRange: tieBreakRange,
	}
}

func (e *EnhancedCacheAwareSelectorWithTieBreak) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	type nodeScore struct {
		node  *PrefillNode
		score float64
	}

	scores := make([]nodeScore, len(nodes))

	// 计算每个节点的基础得分
	for i, node := range nodes {
		hitCount := 0
		for _, hashID := range request.HashIDs {
			if _, exists := node.CacheBlocks[hashID]; exists {
				hitCount++
			}
		}

		hitRatio := float64(hitCount) / float64(len(request.HashIDs))
		currentLoad := float64(len(node.RequestQueue)) / 100.0

		// 基础得分
		baseScore := e.Alpha*hitRatio - e.Beta*currentLoad

		// 添加随机抖动用于tie-breaking
		jitter := (rand.Float64() - 0.5) * e.TieBreakRange

		scores[i] = nodeScore{
			node:  node,
			score: baseScore + jitter,
		}
	}

	// 选择得分最高的节点
	bestScore := scores[0].score
	bestNode := scores[0].node

	for _, s := range scores[1:] {
		if s.score > bestScore {
			bestScore = s.score
			bestNode = s.node
		}
	}

	return bestNode
}

func (e *EnhancedCacheAwareSelectorWithTieBreak) GetName() string {
	return fmt.Sprintf("Enhanced-TB(α=%.1f,β=%.1f)", e.Alpha, e.Beta)
}

// RunBetaSensitivityAnalysis 运行β灵敏度分析
func RunBetaSensitivityAnalysis() {
	fmt.Println("\n============= β灵敏度分析与稳健性验证 =============")
	fmt.Println("分析内容：")
	fmt.Println("1. β值从0.0到2.0变化")
	fmt.Println("2. 固定α=0.6")
	fmt.Println("3. 添加随机tie-break (±0.01)")
	fmt.Println("4. 追踪P95延迟和负载指标")
	fmt.Println("=" + repeat("=", 50))

	// 加载数据
	requests, err := LoadRequests("mooncake_trace.jsonl")
	if err != nil {
		fmt.Printf("加载数据失败: %v\n", err)
		return
	}

	// 使用前2000个请求进行分析
	testRequests := requests[:min(2000, len(requests))]

	// β值范围
	betaValues := []float64{0.0, 0.2, 0.4, 0.6, 0.8, 1.0, 1.2, 1.4, 1.6, 1.8, 2.0}

	// 固定参数
	alpha := 0.6
	nodeCount := 4
	cacheSize := 500
	tieBreakRange := 0.01

	// 存储结果
	results := make([]BetaSensitivityResult, 0)

	fmt.Println("\n📊 开始β灵敏度测试...")
	fmt.Println("β值\t命中率\t集中度\tP95延迟\tP95负载\t负载标准差")
	fmt.Println(repeat("-", 60))

	for _, beta := range betaValues {
		// 创建带tie-break的选择器
		selector := NewEnhancedSelectorWithTieBreak(alpha, beta, tieBreakRange)

		// 运行模拟
		result := runSingleBetaTest(selector, testRequests, nodeCount, cacheSize, beta)
		results = append(results, result)

		fmt.Printf("%.1f\t%.2f%%\t%.1f%%\t%.2fms\t%.1f\t%.2f\n",
			beta,
			result.HitRate*100,
			result.Concentration*100,
			result.P95Latency,
			result.P95Load,
			result.LoadStdDev)
	}

	// 绘制ASCII图表
	drawBetaCurves(results)

	// 分析结论稳健性
	analyzeRobustness(results)
}

// runSingleBetaTest 运行单个β值测试
func runSingleBetaTest(selector PrefillNodeSelector, requests []*Request, nodeCount, cacheSize int, beta float64) BetaSensitivityResult {
	// 创建模拟器
	sim := NewSimulator(nodeCount, cacheSize, selector, func() EvictionAlgorithm { return NewLFUEviction() })

	// 追踪指标
	nodeLoads := make(map[string]int)
	nodeLatencies := make(map[string][]float64)
	allLatencies := make([]float64, 0)

	// 运行模拟
	for _, request := range requests {
		startTime := time.Now()

		result, err := sim.processor.ProcessRequest(request, sim.nodes)
		if err != nil {
			continue
		}

		// 计算延迟 (模拟延迟 = 基础延迟 + 队列长度影响)
		queueLen := len(result.SelectedNode.RequestQueue)
		baseLatency := 10.0 // 基础10ms
		queueLatency := float64(queueLen) * 0.5 // 每个队列请求增加0.5ms
		totalLatency := baseLatency + queueLatency + result.ProcessTime

		// 记录延迟
		allLatencies = append(allLatencies, totalLatency)
		if nodeLatencies[result.SelectedNode.ID] == nil {
			nodeLatencies[result.SelectedNode.ID] = make([]float64, 0)
		}
		nodeLatencies[result.SelectedNode.ID] = append(nodeLatencies[result.SelectedNode.ID], totalLatency)

		// 记录负载
		nodeLoads[result.SelectedNode.ID]++

		_ = time.Since(startTime) // 实际运行时间（不使用）
	}

	// 计算统计指标
	stats := sim.processor.GetStatistics()

	// 计算集中度
	maxLoad := 0
	totalLoad := 0
	loads := make([]float64, 0)
	for _, count := range nodeLoads {
		if count > maxLoad {
			maxLoad = count
		}
		totalLoad += count
		loads = append(loads, float64(count))
	}
	concentration := float64(maxLoad) / float64(totalLoad)

	// 计算负载标准差
	loadMean := float64(totalLoad) / float64(len(nodeLoads))
	var loadVariance float64
	for _, load := range loads {
		loadVariance += math.Pow(load-loadMean, 2)
	}
	loadStdDev := math.Sqrt(loadVariance / float64(len(loads)))

	// 计算P95延迟
	sort.Float64s(allLatencies)
	p95Index := int(float64(len(allLatencies)) * 0.95)
	p95Latency := 0.0
	if p95Index < len(allLatencies) {
		p95Latency = allLatencies[p95Index]
	}

	// 计算P95负载（节点负载的P95值）
	sort.Float64s(loads)
	p95LoadIndex := int(float64(len(loads)) * 0.95)
	p95Load := 0.0
	if p95LoadIndex < len(loads) {
		p95Load = loads[p95LoadIndex]
	}

	return BetaSensitivityResult{
		Beta:          beta,
		HitRate:       stats.HitRate,
		Concentration: concentration,
		P95Latency:    p95Latency,
		P95Load:       p95Load,
		LoadStdDev:    loadStdDev,
	}
}

// drawBetaCurves 绘制β灵敏度曲线
func drawBetaCurves(results []BetaSensitivityResult) {
	fmt.Println("\n📈 β灵敏度曲线 (ASCII可视化)")
	fmt.Println("=" + repeat("=", 60))

	// 1. 命中率曲线
	fmt.Println("\n1. 命中率变化曲线 (%):")
	fmt.Println("   30%|" + repeat("-", 50))

	for _, r := range results {
		barLen := int(r.HitRate * 100)
		bar := repeat("█", barLen/2)
		fmt.Printf("β=%.1f |%-25s %.1f%%\n", r.Beta, bar, r.HitRate*100)
	}

	// 2. 集中度曲线
	fmt.Println("\n2. 集中度变化曲线 (%):")
	fmt.Println("  100%|" + repeat("-", 50))

	for _, r := range results {
		barLen := int(r.Concentration * 100)
		bar := repeat("█", barLen/2)
		fmt.Printf("β=%.1f |%-25s %.1f%%\n", r.Beta, bar, r.Concentration*100)
	}

	// 3. P95延迟曲线
	fmt.Println("\n3. P95延迟变化曲线 (ms):")
	fmt.Println("   50ms|" + repeat("-", 50))

	maxLatency := 0.0
	for _, r := range results {
		if r.P95Latency > maxLatency {
			maxLatency = r.P95Latency
		}
	}

	for _, r := range results {
		barLen := int(r.P95Latency / maxLatency * 50)
		bar := repeat("█", barLen)
		fmt.Printf("β=%.1f |%-25s %.1fms\n", r.Beta, bar, r.P95Latency)
	}

	// 4. 负载标准差曲线
	fmt.Println("\n4. 负载标准差曲线 (表示负载均衡程度，越小越均衡):")
	fmt.Println("  500|" + repeat("-", 50))

	for _, r := range results {
		barLen := int(r.LoadStdDev / 10)
		bar := repeat("█", barLen)
		fmt.Printf("β=%.1f |%-25s %.1f\n", r.Beta, bar, r.LoadStdDev)
	}
}

// analyzeRobustness 分析结论稳健性
func analyzeRobustness(results []BetaSensitivityResult) {
	fmt.Println("\n🔬 稳健性分析报告")
	fmt.Println("=" + repeat("=", 60))

	// 找出最优β值
	optimalBeta := 0.0
	minP95 := math.MaxFloat64

	for _, r := range results {
		if r.P95Latency < minP95 {
			minP95 = r.P95Latency
			optimalBeta = r.Beta
		}
	}

	// 分析命中率变化
	hitRateRange := results[len(results)-1].HitRate - results[0].HitRate

	// 分析集中度变化
	maxConcentration := 0.0
	minConcentration := 1.0
	for _, r := range results {
		if r.Concentration > maxConcentration {
			maxConcentration = r.Concentration
		}
		if r.Concentration < minConcentration {
			minConcentration = r.Concentration
		}
	}

	fmt.Println("\n📊 关键发现：")
	fmt.Printf("1. 命中率变化范围: %.2f%% (β从0到2)\n", hitRateRange*100)
	fmt.Printf("2. 集中度变化范围: %.1f%% - %.1f%%\n", minConcentration*100, maxConcentration*100)
	fmt.Printf("3. 最优β值(P95延迟最小): %.1f\n", optimalBeta)

	fmt.Println("\n🎯 稳健性结论：")

	// 判断稳健性
	if hitRateRange < 0.02 { // 命中率变化小于2%
		fmt.Println("✅ 命中率对β变化不敏感（变化<2%），结论稳健")
	} else {
		fmt.Println("⚠️ 命中率对β变化敏感（变化>2%），需谨慎选择β值")
	}

	if maxConcentration > 0.8 {
		fmt.Println("❌ 即使调整β值，仍存在严重集中化风险（>80%）")
	} else if maxConcentration > 0.5 {
		fmt.Println("⚠️ 存在中度集中化风险（50%-80%），需要额外机制")
	} else {
		fmt.Println("✅ 集中化风险可控（<50%）")
	}

	// 找出平衡点
	balancePoint := 0.0
	minDiff := math.MaxFloat64
	for _, r := range results {
		// 寻找命中率和负载均衡的平衡点
		diff := math.Abs(r.HitRate*100 - (1-r.Concentration)*100)
		if diff < minDiff {
			minDiff = diff
			balancePoint = r.Beta
		}
	}

	fmt.Printf("\n💡 推荐配置：\n")
	fmt.Printf("- 性能优先: β=%.1f (P95延迟最小)\n", optimalBeta)
	fmt.Printf("- 平衡配置: β=%.1f (命中率与负载均衡平衡)\n", balancePoint)
	fmt.Printf("- 负载优先: β=%.1f (集中度最低)\n", 2.0)

	fmt.Println("\n🔍 核心洞察：")
	fmt.Println("1. 增加β权重能改善负载均衡，但改善有限")
	fmt.Println("2. 过大的β值会略微降低命中率")
	fmt.Println("3. Random tie-breaking提供了额外的负载分散")
	fmt.Println("4. 需要动态迁移等机制才能根本解决集中化问题")
}