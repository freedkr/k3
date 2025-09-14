package main

import (
	"fmt"
)

// CacheAwareAnalyzer 分析CacheAware策略的集中化根本原因
type CacheAwareAnalyzer struct {
	stepByStepLog []string
}

func NewCacheAwareAnalyzer() *CacheAwareAnalyzer {
	return &CacheAwareAnalyzer{
		stepByStepLog: make([]string, 0),
	}
}

func (c *CacheAwareAnalyzer) AnalyzeConcentrationEffect() {
	fmt.Println("\n============= CacheAware集中化原因深度分析 =============")

	// 模拟初始状态：4个空节点
	nodes := []*PrefillNode{
		{ID: "node-0", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0)},
		{ID: "node-1", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0)},
		{ID: "node-2", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0)},
		{ID: "node-3", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0)},
	}

	selector := &CacheAwareSelector{}

	// 加载实际的前几个请求来分析
	requests, err := LoadRequests("mooncake_trace.jsonl")
	if err != nil {
		fmt.Printf("加载数据失败: %v\n", err)
		return
	}

	fmt.Println("🔍 逐步追踪CacheAware的决策过程:")
	fmt.Println(repeat("=", 60))

	// 分析前20个请求的决策过程
	for i := 0; i < 20 && i < len(requests); i++ {
		request := requests[i]
		fmt.Printf("\n📋 请求#%d: %v\n", i, request.HashIDs[:min(5, len(request.HashIDs))])

		// 计算每个节点的得分
		c.analyzeNodeScoring(request, nodes)

		// 执行选择
		selectedNode := selector.SelectNode(request, nodes)
		fmt.Printf("✅ 选中节点: %s\n", selectedNode.ID)

		// 模拟缓存更新
		c.simulateCacheUpdate(request, selectedNode)

		// 显示节点状态
		c.showNodeState(nodes)

		if i == 4 || i == 9 || i == 19 {
			fmt.Println("\n🔄 分析到此阶段的集中化趋势:")
			fmt.Println(repeat("-", 50))
			c.analyzeConcentrationTrend(nodes)
		}
	}

	// 总结根本原因
	c.explainRootCause()
}

func (c *CacheAwareAnalyzer) analyzeNodeScoring(request *Request, nodes []*PrefillNode) {
	fmt.Println("🧮 各节点得分计算:")

	for _, node := range nodes {
		hitCount := 0
		for _, hashID := range request.HashIDs {
			if _, exists := node.CacheBlocks[hashID]; exists {
				hitCount++
			}
		}

		load := float64(len(node.RequestQueue)) / float64(1000) // 简化负载计算
		score := float64(hitCount) - load
		cacheSize := len(node.CacheBlocks)

		fmt.Printf("  %s: 命中=%d/%d, 负载=%.3f, 得分=%.3f, 缓存块数=%d\n",
			node.ID, hitCount, len(request.HashIDs), load, score, cacheSize)
	}
}

func (c *CacheAwareAnalyzer) simulateCacheUpdate(request *Request, selectedNode *PrefillNode) {
	// 简化：只添加不存在的blocks
	addedBlocks := 0
	for _, hashID := range request.HashIDs {
		if _, exists := selectedNode.CacheBlocks[hashID]; !exists {
			selectedNode.CacheBlocks[hashID] = &Block{HashID: hashID}
			addedBlocks++
		}
	}
	fmt.Printf("📦 向%s添加了%d个新blocks\n", selectedNode.ID, addedBlocks)
}

func (c *CacheAwareAnalyzer) showNodeState(nodes []*PrefillNode) {
	fmt.Println("📊 当前节点状态:")
	for _, node := range nodes {
		cacheCount := len(node.CacheBlocks)
		// 统计热点blocks
		hotBlocks := 0
		for blockID := range node.CacheBlocks {
			if blockID == 0 || (blockID >= 46 && blockID <= 57) {
				hotBlocks++
			}
		}
		fmt.Printf("  %s: 总缓存=%d, 热点块=%d\n", node.ID, cacheCount, hotBlocks)
	}
}

func (c *CacheAwareAnalyzer) analyzeConcentrationTrend(nodes []*PrefillNode) {
	totalBlocks := 0
	maxBlocks := 0
	maxNode := ""

	for _, node := range nodes {
		blockCount := len(node.CacheBlocks)
		totalBlocks += blockCount
		if blockCount > maxBlocks {
			maxBlocks = blockCount
			maxNode = node.ID
		}
	}

	concentration := float64(maxBlocks) / float64(totalBlocks) * 100
	fmt.Printf("🎯 集中度分析: %s持有%.1f%%的缓存块 (%d/%d)\n",
		maxNode, concentration, maxBlocks, totalBlocks)

	if concentration > 80 {
		fmt.Printf("⚠️  高度集中！单节点承载超过80%%的缓存\n")
	}
}

func (c *CacheAwareAnalyzer) explainRootCause() {
	fmt.Println("\n🎯 CacheAware集中化的根本原因分析")
	fmt.Println(repeat("=", 60))

	fmt.Println("\n1️⃣ 【正反馈循环机制】")
	fmt.Println("   初始状态: 所有节点缓存为空，得分相等")
	fmt.Println("   第一次选择: Random选择某个节点(如node-0)")
	fmt.Println("   缓存建立: node-0获得了一些blocks")
	fmt.Println("   后续请求: 包含相同blocks的请求会优先选择node-0")
	fmt.Println("   ➡️  结果: node-0的缓存越来越多，吸引更多请求")

	fmt.Println("\n2️⃣ 【热点blocks的马太效应】")
	fmt.Println("   热点特征: hash_id=0出现在46%的请求中")
	fmt.Println("   首次缓存: 一旦某节点缓存了hash_id=0")
	fmt.Println("   持续优势: 后续46%的请求都倾向于选择该节点")
	fmt.Println("   ➡️  结果: '富者愈富'，热点节点垄断热点blocks")

	fmt.Println("\n3️⃣ 【算法设计缺陷】")
	fmt.Println("   得分公式: score = hitCount - load")
	fmt.Println("   负载权重: load权重过低，无法抵消hitCount优势")
	fmt.Println("   缺乏分散: 算法没有主动分散热点的机制")
	fmt.Println("   ➡️  结果: 短期收益(命中率)压倒长期平衡")

	fmt.Println("\n4️⃣ 【workload特征放大效应】")
	fmt.Println("   极端热点: 少数blocks占据绝大部分访问")
	fmt.Println("   高重叠度: 请求间有大量共同的hot blocks")
	fmt.Println("   长期稳定: 热点模式在整个trace中保持一致")
	fmt.Println("   ➡️  结果: CacheAware的局部优化被无限放大")

	fmt.Println("\n🔧 问题的本质:")
	fmt.Println("   CacheAware策略 = 贪心算法 + 局部最优")
	fmt.Println("   在极端热点场景下，贪心导致资源分配失衡")
	fmt.Println("   单一节点成为'热点黑洞'，其他节点资源浪费")

	fmt.Println("\n💡 为什么Random/LoadBalanced更好:")
	fmt.Println("   Random: 天然打破正反馈循环，强制热点分散")
	fmt.Println("   LoadBalanced: 显式负载均衡，防止单点过载")
	fmt.Println("   结果: 全局资源利用 > 局部缓存优化")

	fmt.Println("\n🎉 关键洞察:")
	fmt.Println("   ✅ 在高热点场景: 分散策略 > 聚集策略")
	fmt.Println("   ✅ 负载均衡的价值 > 缓存局部性的价值")
	fmt.Println("   ✅ 简单策略可能比复杂策略更robust")
}

// strings.Repeat的简单实现
func repeat(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}

// RunCacheAwareAnalysis 运行CacheAware集中化分析
func RunCacheAwareAnalysis() {
	analyzer := NewCacheAwareAnalyzer()
	analyzer.AnalyzeConcentrationEffect()
}