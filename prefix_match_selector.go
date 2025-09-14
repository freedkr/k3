package main

import (
	"fmt"
	"strconv"
	"strings"
)

// LongestPrefixMatchSelector 最长前缀匹配选择器
type LongestPrefixMatchSelector struct {
	name string
}

func NewLongestPrefixMatchSelector() *LongestPrefixMatchSelector {
	return &LongestPrefixMatchSelector{
		name: "LongestPrefixMatch",
	}
}

func (l *LongestPrefixMatchSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	type nodeMatchResult struct {
		node               *PrefillNode
		longestPrefixLen   int     // 最长前缀匹配长度
		totalHitCount      int     // 总命中数(用于tie-breaking)
		finalScore         float64 // 最终得分
	}

	results := make([]nodeMatchResult, len(nodes))

	// 分析每个节点的匹配情况
	for i, node := range nodes {
		longestPrefix, totalHits := l.calculateNodeMatch(request, node)
		load := float64(len(node.RequestQueue)) / float64(node.MaxCacheSize)

		// 评分公式: 前缀长度权重更高 + 总命中数作为tie-breaker - 负载
		finalScore := float64(longestPrefix)*2.0 + float64(totalHits)*0.5 - load

		results[i] = nodeMatchResult{
			node:               node,
			longestPrefixLen:   longestPrefix,
			totalHitCount:      totalHits,
			finalScore:         finalScore,
		}
	}

	// 选择得分最高的节点
	bestResult := results[0]
	for _, result := range results[1:] {
		if result.finalScore > bestResult.finalScore {
			bestResult = result
		}
	}

	return bestResult.node
}

func (l *LongestPrefixMatchSelector) calculateNodeMatch(request *Request, node *PrefillNode) (int, int) {
	// 1. 构建节点缓存的所有前缀
	cachedPrefixes := l.buildPrefixMap(node)

	// 2. 寻找最长前缀匹配
	longestPrefixLen := l.findLongestPrefixMatch(request.HashIDs, cachedPrefixes)

	// 3. 计算总命中数（用于tie-breaking）
	totalHits := 0
	for _, hashID := range request.HashIDs {
		if _, exists := node.CacheBlocks[hashID]; exists {
			totalHits++
		}
	}

	return longestPrefixLen, totalHits
}

func (l *LongestPrefixMatchSelector) buildPrefixMap(node *PrefillNode) map[string]bool {
	prefixes := make(map[string]bool)

	// 从缓存的blocks构建所有可能的前缀
	// 这里简化处理，假设缓存中的连续hash_id构成前缀
	hashIDs := make([]int, 0, len(node.CacheBlocks))
	for hashID := range node.CacheBlocks {
		hashIDs = append(hashIDs, hashID)
	}

	// 简单排序
	for i := 0; i < len(hashIDs); i++ {
		for j := i + 1; j < len(hashIDs); j++ {
			if hashIDs[j] < hashIDs[i] {
				hashIDs[i], hashIDs[j] = hashIDs[j], hashIDs[i]
			}
		}
	}

	// 构建所有可能的前缀
	for i := 1; i <= len(hashIDs) && i <= 10; i++ { // 限制前缀长度避免过度计算
		prefix := l.buildPrefixString(hashIDs[:i])
		prefixes[prefix] = true
	}

	return prefixes
}

// findLongestPrefixMatch 找到最长前缀匹配
func (l *LongestPrefixMatchSelector) findLongestPrefixMatch(requestHashIDs []int, cachedPrefixes map[string]bool) int {
	maxPrefixLen := 0

	// 从最长到最短检查请求的前缀
	for prefixLen := len(requestHashIDs); prefixLen >= 1; prefixLen-- {
		requestPrefix := l.buildPrefixString(requestHashIDs[:prefixLen])
		if cachedPrefixes[requestPrefix] {
			maxPrefixLen = prefixLen
			break
		}
	}

	return maxPrefixLen
}

func (l *LongestPrefixMatchSelector) buildPrefixString(hashIDs []int) string {
	parts := make([]string, len(hashIDs))
	for i, id := range hashIDs {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

func (l *LongestPrefixMatchSelector) GetName() string {
	return l.name
}

// ContinuousPrefixMatchSelector 连续前缀匹配选择器（更严格的前缀要求）
type ContinuousPrefixMatchSelector struct {
	name string
}

func NewContinuousPrefixMatchSelector() *ContinuousPrefixMatchSelector {
	return &ContinuousPrefixMatchSelector{
		name: "ContinuousPrefixMatch",
	}
}

func (c *ContinuousPrefixMatchSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	type nodeMatchResult struct {
		node                 *PrefillNode
		continuousPrefixLen  int     // 连续前缀匹配长度
		scatteredHits        int     // 散列命中数
		finalScore           float64 // 最终得分
	}

	results := make([]nodeMatchResult, len(nodes))

	for i, node := range nodes {
		continuousLen, scatteredHits := c.analyzeContinuousMatch(request, node)
		load := float64(len(node.RequestQueue)) / float64(node.MaxCacheSize)

		// 评分: 连续前缀权重最高 + 散列命中 - 负载
		finalScore := float64(continuousLen)*3.0 + float64(scatteredHits)*0.3 - load

		results[i] = nodeMatchResult{
			node:                node,
			continuousPrefixLen: continuousLen,
			scatteredHits:       scatteredHits,
			finalScore:          finalScore,
		}
	}

	// 选择得分最高的节点
	bestResult := results[0]
	for _, result := range results[1:] {
		if result.finalScore > bestResult.finalScore {
			bestResult = result
		}
	}

	return bestResult.node
}

func (c *ContinuousPrefixMatchSelector) analyzeContinuousMatch(request *Request, node *PrefillNode) (int, int) {
	// 1. 寻找从开头开始的连续匹配长度
	continuousLen := 0
	for i, hashID := range request.HashIDs {
		if _, exists := node.CacheBlocks[hashID]; exists {
			continuousLen = i + 1
		} else {
			break // 一旦不连续就停止
		}
	}

	// 2. 计算剩余的散列命中数
	scatteredHits := 0
	for i := continuousLen; i < len(request.HashIDs); i++ {
		if _, exists := node.CacheBlocks[request.HashIDs[i]]; exists {
			scatteredHits++
		}
	}

	return continuousLen, scatteredHits
}

func (c *ContinuousPrefixMatchSelector) GetName() string {
	return c.name
}

// PrefixMatchComparator 前缀匹配对比分析器
type PrefixMatchComparator struct {
	simpleSelector     *CacheAwareSelector
	prefixSelector     *LongestPrefixMatchSelector
	continuousSelector *ContinuousPrefixMatchSelector
}

func NewPrefixMatchComparator() *PrefixMatchComparator {
	return &PrefixMatchComparator{
		simpleSelector:     &CacheAwareSelector{},
		prefixSelector:     NewLongestPrefixMatchSelector(),
		continuousSelector: NewContinuousPrefixMatchSelector(),
	}
}

func (p *PrefixMatchComparator) CompareStrategies(requests []*Request) {
	fmt.Println("\n============= 前缀匹配 vs 简单匹配 对比分析 =============")

	// 创建测试节点
	nodes := []*PrefillNode{
		{ID: "node-0", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0), MaxCacheSize: 500},
		{ID: "node-1", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0), MaxCacheSize: 500},
		{ID: "node-2", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0), MaxCacheSize: 500},
		{ID: "node-3", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0), MaxCacheSize: 500},
	}

	// 测试不同的选择器策略
	strategies := []struct {
		name     string
		selector PrefillNodeSelector
	}{
		{"简单命中匹配", p.simpleSelector},
		{"最长前缀匹配", p.prefixSelector},
		{"连续前缀匹配", p.continuousSelector},
	}

	fmt.Printf("测试前%d个请求的选择差异:\n\n", min(1000, len(requests)))

	for _, strategy := range strategies {
		fmt.Printf("🎯 策略: %s\n", strategy.name)

		// 重置节点状态
		for _, node := range nodes {
			node.CacheBlocks = make(map[int]*Block)
			node.RequestQueue = make([]*Request, 0)
		}

		result := p.testStrategy(strategy.selector, requests[:min(1000, len(requests))], nodes)
		p.printStrategyResult(strategy.name, result)
		fmt.Println()
	}

	// 详细对比分析
	p.detailedComparisonAnalysis(requests[:min(100, len(requests))], nodes)
}

type StrategyTestResult struct {
	HitRate            float64
	NodeDistribution   map[string]int
	ConcentrationRatio float64
	SelectionDetails   []string // 前10个选择的详细信息
}

func (p *PrefixMatchComparator) testStrategy(selector PrefillNodeSelector, requests []*Request, nodes []*PrefillNode) StrategyTestResult {
	totalHits := 0
	totalAccess := 0
	selectionDetails := make([]string, 0, 10)

	for i, request := range requests {
		selectedNode := selector.SelectNode(request, nodes)

		// 记录前10个选择的详细信息
		if i < 10 {
			detail := fmt.Sprintf("请求#%d -> %s (blocks: %v)", i, selectedNode.ID, request.HashIDs[:min(3, len(request.HashIDs))])
			selectionDetails = append(selectionDetails, detail)
		}

		// 统计命中和添加新blocks
		hits := 0
		for _, hashID := range request.HashIDs {
			if block, exists := selectedNode.CacheBlocks[hashID]; exists {
				hits++
				block.HitCount++
			} else {
				selectedNode.CacheBlocks[hashID] = &Block{
					HashID:    hashID,
					HitCount:  1,
					AccessSeq: i,
					CreateSeq: i,
				}
			}
		}

		totalHits += hits
		totalAccess += len(request.HashIDs)

		// 简单的容量管理
		if len(selectedNode.CacheBlocks) > selectedNode.MaxCacheSize {
			count := 0
			for hashID := range selectedNode.CacheBlocks {
				delete(selectedNode.CacheBlocks, hashID)
				count++
				if count >= 50 {
					break
				}
			}
		}
	}

	// 计算结果统计
	hitRate := float64(totalHits) / float64(totalAccess) * 100

	distribution := make(map[string]int)
	totalBlocks := 0
	maxBlocks := 0
	for _, node := range nodes {
		blockCount := len(node.CacheBlocks)
		distribution[node.ID] = blockCount
		totalBlocks += blockCount
		if blockCount > maxBlocks {
			maxBlocks = blockCount
		}
	}

	concentrationRatio := 0.0
	if totalBlocks > 0 {
		concentrationRatio = float64(maxBlocks) / float64(totalBlocks) * 100
	}

	return StrategyTestResult{
		HitRate:            hitRate,
		NodeDistribution:   distribution,
		ConcentrationRatio: concentrationRatio,
		SelectionDetails:   selectionDetails,
	}
}

func (p *PrefixMatchComparator) printStrategyResult(_ string, result StrategyTestResult) {
	fmt.Printf("   命中率: %.2f%%\n", result.HitRate)
	fmt.Printf("   集中化比例: %.1f%%\n", result.ConcentrationRatio)
	fmt.Printf("   节点分布: ")
	for nodeID, blocks := range result.NodeDistribution {
		fmt.Printf("%s=%d ", nodeID, blocks)
	}
	fmt.Printf("\n   选择示例:\n")
	for _, detail := range result.SelectionDetails {
		fmt.Printf("      %s\n", detail)
	}
}

func (p *PrefixMatchComparator) detailedComparisonAnalysis(requests []*Request, nodes []*PrefillNode) {
	fmt.Printf("============= 选择差异详细分析 =============\n\n")

	// 逐请求对比前10个请求的选择差异
	fmt.Printf("前10个请求的选择对比:\n")
	fmt.Printf("%-8s %-15s %-18s %-18s\n", "请求#", "简单匹配", "最长前缀匹配", "连续前缀匹配")
	fmt.Printf("%s\n", strings.Repeat("-", 70))

	for i := 0; i < min(10, len(requests)) && i < 10; i++ {
		request := requests[i]

		// 重置节点状态（简化处理）
		for _, node := range nodes {
			node.CacheBlocks = make(map[int]*Block)
			// 模拟一些初始缓存状态
			if i > 0 {
				for j := 0; j < min(i*2, 10); j++ {
					node.CacheBlocks[j] = &Block{HashID: j, HitCount: j + 1}
				}
			}
		}

		simpleChoice := p.simpleSelector.SelectNode(request, nodes)
		prefixChoice := p.prefixSelector.SelectNode(request, nodes)
		continuousChoice := p.continuousSelector.SelectNode(request, nodes)

		fmt.Printf("%-8d %-15s %-18s %-18s",
			i, simpleChoice.ID, prefixChoice.ID, continuousChoice.ID)

		// 标记差异
		if simpleChoice.ID != prefixChoice.ID || prefixChoice.ID != continuousChoice.ID {
			fmt.Printf(" 🔍")
		}
		fmt.Printf("\n")
	}

	fmt.Printf("\n💡 差异分析:\n")
	fmt.Printf("• 简单匹配: 基于散列命中数量，忽略顺序关系\n")
	fmt.Printf("• 最长前缀匹配: 寻找最长连续序列匹配，权重更高\n")
	fmt.Printf("• 连续前缀匹配: 要求从头开始连续匹配，最严格\n")
	fmt.Printf("• 🔍 表示三种策略选择结果不同\n")
}

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RunPrefixMatchComparison 运行前缀匹配对比测试
func RunPrefixMatchComparison() {
	fmt.Println("开始前缀匹配 vs 简单匹配对比测试...")

	// 加载数据
	requests, err := LoadRequests("mooncake_trace.jsonl")
	if err != nil {
		fmt.Printf("加载数据失败: %v\n", err)
		return
	}

	comparator := NewPrefixMatchComparator()
	comparator.CompareStrategies(requests)
}