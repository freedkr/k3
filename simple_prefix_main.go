package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// 简化版的数据结构定义
type SimpleRequest struct {
	HashIDs []int `json:"hash_ids"`
}

type SimpleBlock struct {
	HashID   int
	HitCount int
}

type SimpleNode struct {
	ID           string
	CacheBlocks  map[int]*SimpleBlock
	RequestQueue []*SimpleRequest
	MaxCacheSize int
}

// 简单命中匹配策略
func simpleMatch(request *SimpleRequest, nodes []*SimpleNode) *SimpleNode {
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

// 前缀匹配策略
func prefixMatch(request *SimpleRequest, nodes []*SimpleNode) *SimpleNode {
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

// 连续前缀匹配策略
func continuousMatch(request *SimpleRequest, nodes []*SimpleNode) *SimpleNode {
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

func loadSimpleRequests(filename string) ([]*SimpleRequest, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var requests []*SimpleRequest
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var request SimpleRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			continue
		}

		requests = append(requests, &request)
	}

	return requests, scanner.Err()
}

func runStrategyTest(strategyName string, strategyFunc func(*SimpleRequest, []*SimpleNode) *SimpleNode, requests []*SimpleRequest) {
	fmt.Printf("\n🎯 测试策略: %s\n", strategyName)

	// 创建测试节点
	nodes := []*SimpleNode{
		{ID: "node-0", CacheBlocks: make(map[int]*SimpleBlock), RequestQueue: make([]*SimpleRequest, 0), MaxCacheSize: 500},
		{ID: "node-1", CacheBlocks: make(map[int]*SimpleBlock), RequestQueue: make([]*SimpleRequest, 0), MaxCacheSize: 500},
		{ID: "node-2", CacheBlocks: make(map[int]*SimpleBlock), RequestQueue: make([]*SimpleRequest, 0), MaxCacheSize: 500},
		{ID: "node-3", CacheBlocks: make(map[int]*SimpleBlock), RequestQueue: make([]*SimpleRequest, 0), MaxCacheSize: 500},
	}

	totalHits := 0
	totalAccess := 0
	testRequests := 1000
	if len(requests) < testRequests {
		testRequests = len(requests)
	}

	// 显示前10个请求的选择
	fmt.Printf("前10个请求的选择:\n")

	for i, request := range requests[:testRequests] {
		selectedNode := strategyFunc(request, nodes)

		if i < 10 {
			fmt.Printf("  请求#%d -> %s (blocks: %v)\n",
				i, selectedNode.ID, request.HashIDs[:min3(3, len(request.HashIDs))])
		}

		// 统计命中和添加新blocks
		hits := 0
		for _, hashID := range request.HashIDs {
			if block, exists := selectedNode.CacheBlocks[hashID]; exists {
				hits++
				block.HitCount++
			} else {
				selectedNode.CacheBlocks[hashID] = &SimpleBlock{
					HashID:   hashID,
					HitCount: 1,
				}
			}
		}

		totalHits += hits
		totalAccess += len(request.HashIDs)

		// 简单容量管理
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

	// 统计结果
	hitRate := float64(totalHits) * 100 / float64(totalAccess)

	// 计算节点分布
	totalBlocks := 0
	maxBlocks := 0
	for _, node := range nodes {
		blockCount := len(node.CacheBlocks)
		totalBlocks += blockCount
		if blockCount > maxBlocks {
			maxBlocks = blockCount
		}
	}

	concentrationRatio := 0.0
	if totalBlocks > 0 {
		concentrationRatio = float64(maxBlocks) / float64(totalBlocks) * 100
	}

	fmt.Printf("命中率: %.2f%%\n", hitRate)
	fmt.Printf("集中化比例: %.1f%%\n", concentrationRatio)
	fmt.Printf("节点分布: ")
	for _, node := range nodes {
		fmt.Printf("%s=%d ", node.ID, len(node.CacheBlocks))
	}
	fmt.Printf("\n")
}

func min3(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main2() {
	fmt.Println("========================================")
	fmt.Println("   前缀匹配 vs 简单匹配 实际对比测试")
	fmt.Println("========================================")

	// 加载数据
	requests, err := loadSimpleRequests("mooncake_trace.jsonl")
	if err != nil {
		fmt.Printf("加载数据失败: %v\n", err)
		return
	}

	fmt.Printf("加载了 %d 个请求\n", len(requests))

	// 分析前几个请求的特征
	fmt.Printf("\n📊 前5个请求的hash_ids特征:\n")
	for i := 0; i < min3(5, len(requests)); i++ {
		fmt.Printf("请求#%d: %v (长度=%d)\n",
			i, requests[i].HashIDs[:min3(8, len(requests[i].HashIDs))], len(requests[i].HashIDs))
	}

	// 测试三种策略
	strategies := []struct {
		name string
		fn   func(*SimpleRequest, []*SimpleNode) *SimpleNode
	}{
		{"简单命中匹配", simpleMatch},
		{"最长前缀匹配", prefixMatch},
		{"连续前缀匹配", continuousMatch},
	}

	for _, strategy := range strategies {
		runStrategyTest(strategy.name, strategy.fn, requests)
	}

	// 详细对比分析
	fmt.Printf("\n============= 详细选择对比 =============\n")

	// 创建新的测试节点用于对比
	nodes := []*SimpleNode{
		{ID: "node-0", CacheBlocks: make(map[int]*SimpleBlock), RequestQueue: make([]*SimpleRequest, 0), MaxCacheSize: 500},
		{ID: "node-1", CacheBlocks: make(map[int]*SimpleBlock), RequestQueue: make([]*SimpleRequest, 0), MaxCacheSize: 500},
		{ID: "node-2", CacheBlocks: make(map[int]*SimpleBlock), RequestQueue: make([]*SimpleRequest, 0), MaxCacheSize: 500},
		{ID: "node-3", CacheBlocks: make(map[int]*SimpleBlock), RequestQueue: make([]*SimpleRequest, 0), MaxCacheSize: 500},
	}

	// 预先在不同节点放置一些缓存数据用于测试
	// node-0: 0,1,2,3,4
	for i := 0; i < 5; i++ {
		nodes[0].CacheBlocks[i] = &SimpleBlock{HashID: i, HitCount: 1}
	}
	// node-1: 5,6,7,8,9
	for i := 5; i < 10; i++ {
		nodes[1].CacheBlocks[i] = &SimpleBlock{HashID: i, HitCount: 1}
	}
	// node-2: 散列的blocks: 0,3,7,12
	scatteredBlocks := []int{0, 3, 7, 12}
	for _, id := range scatteredBlocks {
		nodes[2].CacheBlocks[id] = &SimpleBlock{HashID: id, HitCount: 1}
	}

	fmt.Printf("预设缓存状态:\n")
	fmt.Printf("node-0: 连续blocks [0,1,2,3,4]\n")
	fmt.Printf("node-1: 连续blocks [5,6,7,8,9]\n")
	fmt.Printf("node-2: 散列blocks [0,3,7,12]\n")
	fmt.Printf("node-3: 空\n\n")

	fmt.Printf("前10个真实请求的三种策略选择对比:\n")
	fmt.Printf("%-8s %-12s %-15s %-15s %-18s\n", "请求#", "请求blocks", "简单匹配", "最长前缀", "连续前缀")
	fmt.Printf("%s\n", "--------------------------------------------------------------------------------")

	for i := 0; i < min3(10, len(requests)); i++ {
		request := requests[i]

		simpleChoice := simpleMatch(request, nodes)
		prefixChoice := prefixMatch(request, nodes)
		continuousChoice := continuousMatch(request, nodes)

		requestStr := fmt.Sprintf("[%d", request.HashIDs[0])
		for j := 1; j < min3(4, len(request.HashIDs)); j++ {
			requestStr += fmt.Sprintf(",%d", request.HashIDs[j])
		}
		if len(request.HashIDs) > 4 {
			requestStr += "..."
		}
		requestStr += "]"

		fmt.Printf("%-8d %-12s %-15s %-15s %-15s",
			i, requestStr, simpleChoice.ID, prefixChoice.ID, continuousChoice.ID)

		// 标记差异
		if simpleChoice.ID != prefixChoice.ID || prefixChoice.ID != continuousChoice.ID {
			fmt.Printf(" 🔍")
		}
		fmt.Printf("\n")
	}

	fmt.Printf("\n💡 关键差异分析:\n")
	fmt.Printf("• 简单匹配: 只看命中数量，不考虑顺序\n")
	fmt.Printf("• 最长前缀匹配: 寻找任意位置的最长连续匹配\n")
	fmt.Printf("• 连续前缀匹配: 要求从头开始的严格连续匹配\n")
	fmt.Printf("• 🔍 表示策略选择有差异的情况\n")
}
