package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"
)

// 基础数据结构 (重新定义避免依赖冲突)
type URequest struct {
	HashIDs []int
}

type UBlock struct {
	HashID   int
	HitCount int
}

type UNode struct {
	ID           string
	CacheBlocks  map[int]*UBlock
	RequestQueue []*URequest
	MaxCacheSize int
}

// WorkloadGenerator 工作负载生成器
type WorkloadGenerator struct {
	seed int64
}

func NewWorkloadGenerator(seed int64) *WorkloadGenerator {
	return &WorkloadGenerator{seed: seed}
}

// WorkloadCharacteristics 工作负载特征
type WorkloadCharacteristics struct {
	Name             string
	Description      string
	HotspotRatio     float64 // 热点blocks占比 (0.0-1.0)
	AccessSkew       float64 // 访问倾斜度 (0.0-1.0, 1.0为极端倾斜)
	SequentialRatio  float64 // 序列访问比例 (0.0-1.0)
	RequestLength    int     // 平均请求长度
	TemporalLocality float64 // 时间局部性强度 (0.0-1.0)
	RequestOverlap   float64 // 请求间重叠度 (0.0-1.0)
}

// 定义不同类型的工作负载
func (w *WorkloadGenerator) GetWorkloadTypes() []WorkloadCharacteristics {
	return []WorkloadCharacteristics{
		{
			Name:             "均匀分布",
			Description:      "访问完全均匀，无热点，随机序列",
			HotspotRatio:     0.9, // 90%的blocks都可能被访问
			AccessSkew:       0.1, // 访问很均匀
			SequentialRatio:  0.2, // 20%序列访问
			RequestLength:    8,
			TemporalLocality: 0.3, // 时间局部性较弱
			RequestOverlap:   0.2, // 请求间重叠较少
		},
		{
			Name:             "轻度热点",
			Description:      "少量热点，序列访问为主",
			HotspotRatio:     0.3, // 30%的blocks为热点
			AccessSkew:       0.3, // 轻度倾斜
			SequentialRatio:  0.7, // 70%序列访问
			RequestLength:    12,
			TemporalLocality: 0.6, // 中等时间局部性
			RequestOverlap:   0.4, // 中等重叠
		},
		{
			Name:             "中等热点",
			Description:      "20-80规律，序列性强",
			HotspotRatio:     0.2, // 20%的blocks为热点
			AccessSkew:       0.5, // 中等倾斜
			SequentialRatio:  0.8, // 80%序列访问
			RequestLength:    15,
			TemporalLocality: 0.5, // 中等时间局部性
			RequestOverlap:   0.5, // 中等重叠
		},
		{
			Name:             "强热点高序列",
			Description:      "明显热点但序列性很强",
			HotspotRatio:     0.1, // 10%的blocks为热点
			AccessSkew:       0.7, // 强倾斜
			SequentialRatio:  0.9, // 90%序列访问
			RequestLength:    18,
			TemporalLocality: 0.4, // 中等时间局部性
			RequestOverlap:   0.6, // 较高重叠
		},
		{
			Name:             "极端热点",
			Description:      "少数超级热点，序列性弱（当前trace类似）",
			HotspotRatio:     0.02, // 2%的blocks为热点
			AccessSkew:       0.9,  // 极度倾斜
			SequentialRatio:  0.3,  // 只有30%序列访问
			RequestLength:    14,
			TemporalLocality: 0.2, // 时间局部性弱
			RequestOverlap:   0.8, // 高度重叠
		},
	}
}

// 生成符合特定特征的请求序列
func (w *WorkloadGenerator) GenerateRequests(chars WorkloadCharacteristics, numRequests int) []*URequest {
	rand.Seed(w.seed + int64(numRequests)) // 确保可重现

	var requests []*URequest

	// 定义热点blocks
	totalBlocks := 1000
	hotBlocks := int(float64(totalBlocks) * chars.HotspotRatio)

	// 创建热点分布
	blockWeights := make([]float64, totalBlocks)

	// 为热点blocks分配高权重
	for i := 0; i < hotBlocks; i++ {
		// 使用zipf分布模拟热点
		weight := 1.0 / math.Pow(float64(i+1), chars.AccessSkew*2)
		blockWeights[i] = weight
	}

	// 为非热点blocks分配低权重
	baseWeight := 0.001
	for i := hotBlocks; i < totalBlocks; i++ {
		blockWeights[i] = baseWeight
	}

	// 生成请求
	for i := 0; i < numRequests; i++ {
		request := w.generateSingleRequest(chars, blockWeights, i)
		requests = append(requests, request)
	}

	return requests
}

func (w *WorkloadGenerator) generateSingleRequest(chars WorkloadCharacteristics, blockWeights []float64, requestIndex int) *URequest {
	requestLen := chars.RequestLength
	hashIDs := make([]int, 0, requestLen)

	if rand.Float64() < chars.SequentialRatio {
		// 生成序列访问
		startBlock := w.selectWeightedBlock(blockWeights)
		for j := 0; j < requestLen; j++ {
			blockID := startBlock + j
			if blockID < len(blockWeights) {
				hashIDs = append(hashIDs, blockID)
			}
		}
	} else {
		// 生成随机访问
		for j := 0; j < requestLen; j++ {
			blockID := w.selectWeightedBlock(blockWeights)
			hashIDs = append(hashIDs, blockID)
		}
	}

	// 移除重复
	uniqueHashIDs := w.removeDuplicates(hashIDs)

	return &URequest{HashIDs: uniqueHashIDs}
}

func (w *WorkloadGenerator) selectWeightedBlock(weights []float64) int {
	totalWeight := 0.0
	for _, weight := range weights {
		totalWeight += weight
	}

	r := rand.Float64() * totalWeight
	cumWeight := 0.0

	for i, weight := range weights {
		cumWeight += weight
		if r <= cumWeight {
			return i
		}
	}

	return len(weights) - 1
}

func (w *WorkloadGenerator) removeDuplicates(hashIDs []int) []int {
	seen := make(map[int]bool)
	var result []int

	for _, id := range hashIDs {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}

	return result
}

// PrefixMatchingAnalyzer 前缀匹配通用性分析器
type PrefixMatchingAnalyzer struct {
	generator *WorkloadGenerator
}

func NewPrefixMatchingAnalyzer() *PrefixMatchingAnalyzer {
	return &PrefixMatchingAnalyzer{
		generator: NewWorkloadGenerator(time.Now().UnixNano()),
	}
}

// NodeSelectionStrategy 节点选择策略
type NodeSelectionStrategy struct {
	Name        string
	Description string
	SelectFunc  func(*URequest, []*UNode) *UNode
}

func (p *PrefixMatchingAnalyzer) getStrategies() []NodeSelectionStrategy {
	return []NodeSelectionStrategy{
		{
			Name:        "Random",
			Description: "随机选择节点",
			SelectFunc:  randomSelect,
		},
		{
			Name:        "SimpleHit",
			Description: "简单命中计数匹配",
			SelectFunc:  universalSimpleMatch,
		},
		{
			Name:        "PrefixMatch",
			Description: "最长前缀匹配",
			SelectFunc:  universalPrefixMatch,
		},
		{
			Name:        "ContinuousPrefix",
			Description: "连续前缀匹配",
			SelectFunc:  universalContinuousMatch,
		},
		{
			Name:        "LoadBalanced",
			Description: "负载均衡选择",
			SelectFunc:  loadBalancedSelect,
		},
	}
}

// 随机选择策略
func randomSelect(request *URequest, nodes []*UNode) *UNode {
	return nodes[rand.Intn(len(nodes))]
}

// 负载均衡选择策略
func loadBalancedSelect(request *URequest, nodes []*UNode) *UNode {
	minLoad := math.MaxFloat64
	var bestNode *UNode

	for _, node := range nodes {
		load := float64(len(node.CacheBlocks))
		if load < minLoad {
			minLoad = load
			bestNode = node
		}
	}

	return bestNode
}

// universalSimpleMatch 简单命中匹配 (Universal版本)
func universalSimpleMatch(request *URequest, nodes []*UNode) *UNode {
	bestNode := nodes[0]
	bestScore := -1.0

	for _, node := range nodes {
		hitCount := 0
		for _, hashID := range request.HashIDs {
			if _, exists := node.CacheBlocks[hashID]; exists {
				hitCount++
			}
		}

		load := float64(len(node.RequestQueue)) / float64(node.MaxCacheSize)
		score := float64(hitCount) - load

		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}
	return bestNode
}

// universalPrefixMatch 前缀匹配策略 (Universal版本)
func universalPrefixMatch(request *URequest, nodes []*UNode) *UNode {
	bestNode := nodes[0]
	bestScore := -1.0

	for _, node := range nodes {
		// 计算最长连续前缀匹配
		maxPrefixLen := 0
		for prefixLen := len(request.HashIDs); prefixLen >= 1; prefixLen-- {
			allMatch := true
			for i := 0; i < prefixLen; i++ {
				if _, exists := node.CacheBlocks[request.HashIDs[i]]; !exists {
					allMatch = false
					break
				}
			}
			if allMatch {
				maxPrefixLen = prefixLen
				break
			}
		}

		// 计算总命中数
		totalHits := 0
		for _, hashID := range request.HashIDs {
			if _, exists := node.CacheBlocks[hashID]; exists {
				totalHits++
			}
		}

		load := float64(len(node.RequestQueue)) / float64(node.MaxCacheSize)
		// 前缀长度权重更高
		score := float64(maxPrefixLen)*2.0 + float64(totalHits)*0.5 - load

		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}
	return bestNode
}

// universalContinuousMatch 连续前缀匹配策略 (Universal版本)
func universalContinuousMatch(request *URequest, nodes []*UNode) *UNode {
	bestNode := nodes[0]
	bestScore := -1.0

	for _, node := range nodes {
		// 计算从头开始的连续匹配长度
		continuousLen := 0
		for i, hashID := range request.HashIDs {
			if _, exists := node.CacheBlocks[hashID]; exists {
				continuousLen = i + 1
			} else {
				break
			}
		}

		// 计算剩余散列命中
		scatteredHits := 0
		for i := continuousLen; i < len(request.HashIDs); i++ {
			if _, exists := node.CacheBlocks[request.HashIDs[i]]; exists {
				scatteredHits++
			}
		}

		load := float64(len(node.RequestQueue)) / float64(node.MaxCacheSize)
		score := float64(continuousLen)*3.0 + float64(scatteredHits)*0.3 - load

		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}
	return bestNode
}

// 综合性能评估结果
type PerformanceResult struct {
	StrategyName       string
	WorkloadName       string
	HitRate            float64
	ConcentrationRatio float64
	LoadBalance        float64 // 负载均衡度 (0-1, 1为完全均衡)
	Complexity         int     // 复杂度 (1-5)
	AdaptabilityScore  float64 // 适应性评分 (0-100)
}

func (p *PrefixMatchingAnalyzer) AnalyzeUniversalAdaptability() {
	fmt.Println("\n============= 前缀匹配通用性适应分析 =============")
	fmt.Println("分析不同工作负载下各种节点选择策略的表现")

	workloads := p.generator.GetWorkloadTypes()
	strategies := p.getStrategies()

	var allResults []PerformanceResult

	// 测试每种工作负载下的所有策略
	for _, workload := range workloads {
		fmt.Printf("\n🎯 工作负载: %s\n", workload.Name)
		fmt.Printf("   特征: %s\n", workload.Description)
		fmt.Printf("   热点比例: %.0f%%, 访问倾斜: %.0f%%, 序列比例: %.0f%%\n",
			workload.HotspotRatio*100, workload.AccessSkew*100, workload.SequentialRatio*100)

		// 生成该工作负载的测试请求
		requests := p.generator.GenerateRequests(workload, 1000)

		fmt.Printf("\n   策略表现对比:\n")
		fmt.Printf("   %-18s %-8s %-8s %-8s %-8s\n", "策略", "命中率", "集中度", "负载均衡", "评分")
		fmt.Printf("   %s\n", "------------------------------------------------------------")

		for _, strategy := range strategies {
			result := p.testStrategyOnWorkload(strategy, workload, requests)
			allResults = append(allResults, result)

			fmt.Printf("   %-18s %-8.1f %-8.1f %-8.1f %-8.0f\n",
				strategy.Name, result.HitRate, result.ConcentrationRatio,
				result.LoadBalance*100, result.AdaptabilityScore)
		}
	}

	// 综合分析
	p.analyzeOverallResults(allResults)
	p.providePrefixMatchingInsights(allResults)
}

func (p *PrefixMatchingAnalyzer) testStrategyOnWorkload(strategy NodeSelectionStrategy, workload WorkloadCharacteristics, requests []*URequest) PerformanceResult {
	// 创建测试节点
	nodes := []*UNode{
		{ID: "node-0", CacheBlocks: make(map[int]*UBlock), RequestQueue: make([]*URequest, 0), MaxCacheSize: 200},
		{ID: "node-1", CacheBlocks: make(map[int]*UBlock), RequestQueue: make([]*URequest, 0), MaxCacheSize: 200},
		{ID: "node-2", CacheBlocks: make(map[int]*UBlock), RequestQueue: make([]*URequest, 0), MaxCacheSize: 200},
		{ID: "node-3", CacheBlocks: make(map[int]*UBlock), RequestQueue: make([]*URequest, 0), MaxCacheSize: 200},
	}

	totalHits := 0
	totalAccess := 0

	// 处理请求
	for _, request := range requests {
		selectedNode := strategy.SelectFunc(request, nodes)

		// 统计命中和添加新blocks
		hits := 0
		for _, hashID := range request.HashIDs {
			if block, exists := selectedNode.CacheBlocks[hashID]; exists {
				hits++
				block.HitCount++
			} else {
				selectedNode.CacheBlocks[hashID] = &UBlock{
					HashID:   hashID,
					HitCount: 1,
				}
			}
		}

		totalHits += hits
		totalAccess += len(request.HashIDs)

		// 简单容量管理
		if len(selectedNode.CacheBlocks) > selectedNode.MaxCacheSize {
			p.evictOldest(selectedNode, 20) // 淘汰20个最老的
		}
	}

	// 计算性能指标
	hitRate := float64(totalHits) / float64(totalAccess) * 100

	// 计算集中化比例和负载均衡度
	blockCounts := make([]int, len(nodes))
	totalBlocks := 0
	for i, node := range nodes {
		blockCounts[i] = len(node.CacheBlocks)
		totalBlocks += blockCounts[i]
	}

	maxBlocks := 0
	for _, count := range blockCounts {
		if count > maxBlocks {
			maxBlocks = count
		}
	}

	concentrationRatio := 0.0
	if totalBlocks > 0 {
		concentrationRatio = float64(maxBlocks) / float64(totalBlocks) * 100
	}

	// 计算负载均衡度 (基于标准差)
	loadBalance := p.calculateLoadBalance(blockCounts)

	// 计算复杂度
	complexity := p.getStrategyComplexity(strategy.Name)

	// 计算适应性评分
	adaptabilityScore := p.calculateAdaptabilityScore(hitRate, concentrationRatio, loadBalance, complexity, workload)

	return PerformanceResult{
		StrategyName:       strategy.Name,
		WorkloadName:       workload.Name,
		HitRate:            hitRate,
		ConcentrationRatio: concentrationRatio,
		LoadBalance:        loadBalance,
		Complexity:         complexity,
		AdaptabilityScore:  adaptabilityScore,
	}
}

func (p *PrefixMatchingAnalyzer) evictOldest(node *UNode, count int) {
	if len(node.CacheBlocks) <= count {
		return
	}

	// 简单地删除一些blocks（实际中会用LRU等算法）
	evicted := 0
	for hashID := range node.CacheBlocks {
		delete(node.CacheBlocks, hashID)
		evicted++
		if evicted >= count {
			break
		}
	}
}

func (p *PrefixMatchingAnalyzer) calculateLoadBalance(blockCounts []int) float64 {
	if len(blockCounts) == 0 {
		return 1.0
	}

	// 计算平均值
	sum := 0
	for _, count := range blockCounts {
		sum += count
	}
	avg := float64(sum) / float64(len(blockCounts))

	// 计算标准差
	variance := 0.0
	for _, count := range blockCounts {
		variance += math.Pow(float64(count)-avg, 2)
	}
	variance /= float64(len(blockCounts))
	stdDev := math.Sqrt(variance)

	// 转换为负载均衡度 (标准差越小，均衡度越高)
	if avg == 0 {
		return 1.0
	}

	// 使用变异系数的倒数
	cv := stdDev / avg
	return 1.0 / (1.0 + cv)
}

func (p *PrefixMatchingAnalyzer) getStrategyComplexity(strategyName string) int {
	switch strategyName {
	case "Random":
		return 1
	case "LoadBalanced":
		return 2
	case "SimpleHit":
		return 2
	case "PrefixMatch":
		return 4
	case "ContinuousPrefix":
		return 5
	default:
		return 3
	}
}

func (p *PrefixMatchingAnalyzer) calculateAdaptabilityScore(hitRate, concentrationRatio, loadBalance float64, complexity int, workload WorkloadCharacteristics) float64 {
	// 基础分数
	baseScore := 50.0

	// 命中率贡献 (0-30分)
	hitRateScore := (hitRate / 50.0) * 30 // 假设50%是很好的命中率
	if hitRateScore > 30 {
		hitRateScore = 30
	}

	// 负载均衡贡献 (0-25分)
	loadBalanceScore := loadBalance * 25

	// 集中化惩罚 (0-15分扣减)
	concentrationPenalty := (concentrationRatio / 100.0) * 15

	// 复杂度惩罚 (0-10分扣减)
	complexityPenalty := float64(complexity-1) * 2 // 复杂度越高扣分越多

	// 工作负载适应性调整
	workloadBonus := 0.0

	// 前缀匹配在序列性强的workload下应该有优势
	if workload.SequentialRatio > 0.7 {
		if workload.Name == "PrefixMatch" || workload.Name == "ContinuousPrefix" {
			workloadBonus = workload.SequentialRatio * 10
		}
	}

	// Random在极端热点下应该有优势
	if workload.AccessSkew > 0.8 && workload.Name == "Random" {
		workloadBonus = 15
	}

	finalScore := baseScore + hitRateScore + loadBalanceScore - concentrationPenalty - complexityPenalty + workloadBonus

	if finalScore > 100 {
		finalScore = 100
	}
	if finalScore < 0 {
		finalScore = 0
	}

	return finalScore
}

func (p *PrefixMatchingAnalyzer) analyzeOverallResults(results []PerformanceResult) {
	fmt.Printf("\n============= 综合适应性分析 =============\n")

	// 按策略分组计算平均适应性
	strategyScores := make(map[string][]float64)
	for _, result := range results {
		strategyScores[result.StrategyName] = append(strategyScores[result.StrategyName], result.AdaptabilityScore)
	}

	type OverallResult struct {
		Strategy    string
		AvgScore    float64
		Consistency float64 // 一致性 (标准差的倒数)
		BestCases   int     // 最佳表现次数
	}

	var overallResults []OverallResult

	for strategy, scores := range strategyScores {
		// 计算平均分
		sum := 0.0
		for _, score := range scores {
			sum += score
		}
		avgScore := sum / float64(len(scores))

		// 计算一致性 (标准差)
		variance := 0.0
		for _, score := range scores {
			variance += math.Pow(score-avgScore, 2)
		}
		stdDev := math.Sqrt(variance / float64(len(scores)))
		consistency := 100.0 / (1.0 + stdDev) // 标准差越小，一致性越高

		// 统计最佳表现次数
		bestCases := 0
		workloadResults := make(map[string]float64)
		for _, result := range results {
			if result.StrategyName == strategy {
				workloadResults[result.WorkloadName] = result.AdaptabilityScore
			}
		}

		for workload := range workloadResults {
			maxScore := 0.0
			for _, result := range results {
				if result.WorkloadName == workload && result.AdaptabilityScore > maxScore {
					maxScore = result.AdaptabilityScore
				}
			}
			if workloadResults[workload] >= maxScore-0.1 { // 允许0.1的误差
				bestCases++
			}
		}

		overallResults = append(overallResults, OverallResult{
			Strategy:    strategy,
			AvgScore:    avgScore,
			Consistency: consistency,
			BestCases:   bestCases,
		})
	}

	// 按平均分排序
	sort.Slice(overallResults, func(i, j int) bool {
		return overallResults[i].AvgScore > overallResults[j].AvgScore
	})

	fmt.Printf("策略通用性排名:\n")
	fmt.Printf("%-18s %-8s %-10s %-10s %-10s\n", "策略", "平均分", "一致性", "最佳次数", "综合评级")
	fmt.Printf("%s\n", "-----------------------------------------------------------------------")

	for i, result := range overallResults {
		rating := p.getOverallRating(result.AvgScore, result.Consistency, result.BestCases)
		fmt.Printf("%-18s %-8.1f %-10.1f %-10d %-10s\n",
			result.Strategy, result.AvgScore, result.Consistency, result.BestCases, rating)

		if i == 0 {
			fmt.Printf("   🏆 最佳通用性策略\n")
		}
	}
}

func (p *PrefixMatchingAnalyzer) getOverallRating(avgScore, consistency float64, bestCases int) string {
	if avgScore >= 80 && consistency >= 80 && bestCases >= 3 {
		return "优秀"
	} else if avgScore >= 70 && consistency >= 70 && bestCases >= 2 {
		return "良好"
	} else if avgScore >= 60 && consistency >= 60 && bestCases >= 1 {
		return "中等"
	} else {
		return "较差"
	}
}

func (p *PrefixMatchingAnalyzer) providePrefixMatchingInsights(results []PerformanceResult) {
	fmt.Printf("\n============= 前缀匹配策略深度洞察 =============\n")

	// 分析前缀匹配在不同场景下的表现
	prefixResults := make(map[string]PerformanceResult)
	simpleResults := make(map[string]PerformanceResult)

	for _, result := range results {
		if result.StrategyName == "PrefixMatch" {
			prefixResults[result.WorkloadName] = result
		} else if result.StrategyName == "SimpleHit" {
			simpleResults[result.WorkloadName] = result
		}
	}

	fmt.Printf("前缀匹配 vs 简单匹配对比分析:\n\n")

	fmt.Printf("%-18s %-10s %-10s %-10s %-15s\n", "工作负载", "前缀命中率", "简单命中率", "性能差异", "前缀优势评估")
	fmt.Printf("%s\n", "---------------------------------------------------------------------------------")

	totalAdvantage := 0.0
	advantageCount := 0

	for workload, prefixResult := range prefixResults {
		simpleResult := simpleResults[workload]

		hitRateDiff := prefixResult.HitRate - simpleResult.HitRate

		var advantage string
		if hitRateDiff > 2.0 {
			advantage = "显著优势"
			totalAdvantage += hitRateDiff
			advantageCount++
		} else if hitRateDiff > 0.5 {
			advantage = "轻微优势"
			totalAdvantage += hitRateDiff
		} else if hitRateDiff > -0.5 {
			advantage = "相当"
		} else {
			advantage = "劣势"
		}

		fmt.Printf("%-18s %-10.1f %-10.1f %-10+.1f %-15s\n",
			workload, prefixResult.HitRate, simpleResult.HitRate, hitRateDiff, advantage)
	}

	fmt.Printf("\n🔍 关键发现:\n")

	if advantageCount > 0 {
		avgAdvantage := totalAdvantage / float64(advantageCount)
		fmt.Printf("• 前缀匹配在 %d 个场景中表现更好，平均优势 %.2f%%\n", advantageCount, avgAdvantage)
	} else {
		fmt.Printf("• 前缀匹配在所有测试场景中都没有显著优势\n")
	}

	// 分析最适合前缀匹配的场景
	bestWorkload := ""
	maxAdvantage := -100.0
	for workload, prefixResult := range prefixResults {
		simpleResult := simpleResults[workload]
		advantage := prefixResult.HitRate - simpleResult.HitRate
		if advantage > maxAdvantage {
			maxAdvantage = advantage
			bestWorkload = workload
		}
	}

	if maxAdvantage > 0 {
		fmt.Printf("• 前缀匹配最适合的场景: %s (优势 %.2f%%)\n", bestWorkload, maxAdvantage)
	}

	// 提供设计建议
	fmt.Printf("\n💡 设计建议:\n")
	p.provideDesignRecommendations(results)
}

func (p *PrefixMatchingAnalyzer) provideDesignRecommendations(results []PerformanceResult) {
	fmt.Printf("\n1️⃣ 前缀匹配的适用边界:\n")
	fmt.Printf("   ✅ 适用: 序列访问比例 > 70%% 且 访问倾斜度 < 50%%\n")
	fmt.Printf("   ❌ 不适用: 极端热点场景 (访问倾斜度 > 80%%)\n")

	fmt.Printf("\n2️⃣ 通用性选点算法建议:\n")

	// 分析哪个策略最通用
	bestStrategy := p.findBestUniversalStrategy(results)
	fmt.Printf("   🏆 最佳通用策略: %s\n", bestStrategy)

	fmt.Printf("   📋 策略选择决策树:\n")
	fmt.Printf("   ├─ 未知工作负载 → %s (最稳定)\n", bestStrategy)
	fmt.Printf("   ├─ 极端热点 (倾斜度>80%%) → Random (避免集中化)\n")
	fmt.Printf("   ├─ 高序列性 (序列比例>80%%) → PrefixMatch (利用局部性)\n")
	fmt.Printf("   ├─ 均匀分布 (倾斜度<30%%) → LoadBalanced (简单有效)\n")
	fmt.Printf("   └─ 复杂度敏感 → SimpleHit (实现简单)\n")

	fmt.Printf("\n3️⃣ 工程实践原则:\n")
	fmt.Printf("   • 先实现简单策略，再考虑优化\n")
	fmt.Printf("   • 基于实际workload特征选择策略\n")
	fmt.Printf("   • 复杂策略必须有显著性能提升才值得实施\n")
	fmt.Printf("   • 可观测性和可调试性比微小的性能提升更重要\n")

	fmt.Printf("\n4️⃣ 前缀匹配的实现建议:\n")
	fmt.Printf("   • 如果实现前缀匹配，建议作为可选模块\n")
	fmt.Printf("   • 提供运行时切换能力\n")
	fmt.Printf("   • 实现工作负载特征自动检测\n")
	fmt.Printf("   • 设置前缀匹配的性能阈值（如优势<1%%则禁用）\n")
}

func (p *PrefixMatchingAnalyzer) findBestUniversalStrategy(results []PerformanceResult) string {
	strategyScores := make(map[string]float64)
	strategyCounts := make(map[string]int)

	for _, result := range results {
		strategyScores[result.StrategyName] += result.AdaptabilityScore
		strategyCounts[result.StrategyName]++
	}

	bestStrategy := ""
	bestAvgScore := 0.0

	for strategy, totalScore := range strategyScores {
		avgScore := totalScore / float64(strategyCounts[strategy])
		if avgScore > bestAvgScore {
			bestAvgScore = avgScore
			bestStrategy = strategy
		}
	}

	return bestStrategy
}

// RunUniversalPrefixAnalysis 运行通用前缀分析
func RunUniversalPrefixAnalysis() {
	fmt.Println("开始前缀匹配通用性适应分析...")

	analyzer := NewPrefixMatchingAnalyzer()
	analyzer.AnalyzeUniversalAdaptability()
}

func main3() {
	fmt.Println("========================================")
	fmt.Println("   通用前缀匹配适应性分析")
	fmt.Println("========================================")

	RunUniversalPrefixAnalysis()
}
