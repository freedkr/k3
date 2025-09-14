package main

import (
	"fmt"
	"math"
	"sort"
)

// HotspotMigrationSelector 带热点迁移的缓存感知选择器
type HotspotMigrationSelector struct {
	Alpha                 float64 // 缓存亲和性权重
	Beta                  float64 // 负载均衡权重
	MigrationThreshold    float64 // 迁移触发阈值 (节点集中度)
	HotspotThreshold      float64 // 热点检测阈值 (访问频率)
	MigrationInterval     int     // 迁移检查间隔 (请求数)

	requestCounter        int     // 请求计数器
	migrationHistory      []MigrationRecord // 迁移历史
}

type MigrationRecord struct {
	RequestId       int
	SourceNode      string
	TargetNode      string
	MigratedBlocks  []int
	Reason          string
}

type NodeConcentration struct {
	NodeId           string
	BlockCount       int
	HotBlockCount    int // 热点block数量
	ConcentrationRatio float64 // 集中度比例
}

func NewHotspotMigrationSelector(alpha, beta, migrationThreshold, hotspotThreshold float64) *HotspotMigrationSelector {
	return &HotspotMigrationSelector{
		Alpha:              alpha,
		Beta:               beta,
		MigrationThreshold: migrationThreshold,
		HotspotThreshold:   hotspotThreshold,
		MigrationInterval:  100, // 每100个请求检查一次迁移
		requestCounter:     0,
		migrationHistory:   make([]MigrationRecord, 0),
	}
}

func (h *HotspotMigrationSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	h.requestCounter++

	// 定期检查是否需要热点迁移
	if h.requestCounter%h.MigrationInterval == 0 {
		h.checkAndMigrateHotspots(nodes)
	}

	// 使用增强的缓存感知策略选择节点
	return h.selectNodeWithHotspotAwareness(request, nodes)
}

func (h *HotspotMigrationSelector) selectNodeWithHotspotAwareness(request *Request, nodes []*PrefillNode) *PrefillNode {
	bestNode := nodes[0]
	bestScore := h.calculateScore(request, nodes[0], nodes)

	for _, node := range nodes[1:] {
		score := h.calculateScore(request, node, nodes)
		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}

	return bestNode
}

func (h *HotspotMigrationSelector) calculateScore(request *Request, node *PrefillNode, allNodes []*PrefillNode) float64 {
	// 1. 计算缓存命中率
	hitCount := 0
	for _, hashID := range request.HashIDs {
		if _, exists := node.CacheBlocks[hashID]; exists {
			hitCount++
		}
	}
	hitRatio := float64(hitCount) / float64(len(request.HashIDs))

	// 2. 计算负载因子
	currentLoad := float64(len(node.RequestQueue)) / float64(node.MaxCacheSize)

	// 3. 计算集中化惩罚因子
	concentrations := h.analyzeConcentration(allNodes)
	var concentration NodeConcentration
	for _, conc := range concentrations {
		if conc.NodeId == node.ID {
			concentration = conc
			break
		}
	}
	concentrationPenalty := 0.0
	if concentration.ConcentrationRatio > h.MigrationThreshold {
		// 对过度集中的节点施加惩罚
		concentrationPenalty = (concentration.ConcentrationRatio - h.MigrationThreshold) * 2.0
	}

	// 4. 综合评分（增加集中化惩罚）
	score := h.Alpha*hitRatio - h.Beta*currentLoad - concentrationPenalty

	return score
}

func (h *HotspotMigrationSelector) checkAndMigrateHotspots(nodes []*PrefillNode) {
	// 1. 分析各节点的集中化程度
	concentrations := h.analyzeConcentration(nodes)

	// 2. 找出需要迁移的节点
	var overloadedNodes []NodeConcentration
	var underloadedNodes []NodeConcentration

	for _, conc := range concentrations {
		if conc.ConcentrationRatio > h.MigrationThreshold {
			overloadedNodes = append(overloadedNodes, conc)
		} else if conc.ConcentrationRatio < h.MigrationThreshold/2 {
			underloadedNodes = append(underloadedNodes, conc)
		}
	}

	// 3. 执行热点迁移
	if len(overloadedNodes) > 0 && len(underloadedNodes) > 0 {
		h.performMigration(overloadedNodes, underloadedNodes, nodes)
	}
}

func (h *HotspotMigrationSelector) analyzeConcentration(nodes []*PrefillNode) []NodeConcentration {
	totalBlocks := 0
	hotBlocksGlobal := make(map[int]int) // hash_id -> 全局访问频率

	// 统计全局block分布和热点
	for _, node := range nodes {
		totalBlocks += len(node.CacheBlocks)
		for hashID, block := range node.CacheBlocks {
			hotBlocksGlobal[hashID] += block.HitCount
		}
	}

	// 识别热点blocks (访问频率超过阈值)
	hotBlocks := make(map[int]bool)
	for hashID, hitCount := range hotBlocksGlobal {
		if float64(hitCount)/float64(h.requestCounter) > h.HotspotThreshold {
			hotBlocks[hashID] = true
		}
	}

	// 计算每个节点的集中化程度
	var concentrations []NodeConcentration
	for _, node := range nodes {
		hotBlockCount := 0
		for hashID := range node.CacheBlocks {
			if hotBlocks[hashID] {
				hotBlockCount++
			}
		}

		concentrationRatio := 0.0
		if totalBlocks > 0 {
			concentrationRatio = float64(len(node.CacheBlocks)) / float64(totalBlocks)
		}

		concentrations = append(concentrations, NodeConcentration{
			NodeId:             node.ID,
			BlockCount:         len(node.CacheBlocks),
			HotBlockCount:      hotBlockCount,
			ConcentrationRatio: concentrationRatio,
		})
	}

	return concentrations
}

func (h *HotspotMigrationSelector) performMigration(overloadedNodes, underloadedNodes []NodeConcentration, nodes []*PrefillNode) {
	// 按集中度排序，优先处理最严重的
	sort.Slice(overloadedNodes, func(i, j int) bool {
		return overloadedNodes[i].ConcentrationRatio > overloadedNodes[j].ConcentrationRatio
	})

	sort.Slice(underloadedNodes, func(i, j int) bool {
		return underloadedNodes[i].ConcentrationRatio < underloadedNodes[j].ConcentrationRatio
	})

	for _, overloaded := range overloadedNodes {
		if len(underloadedNodes) == 0 {
			break
		}

		sourceNode := h.findNodeByID(overloaded.NodeId, nodes)
		if sourceNode == nil {
			continue
		}

		// 选择要迁移的blocks (优先迁移非热点blocks，避免破坏缓存局部性)
		blocksToMigrate := h.selectBlocksForMigration(sourceNode, 0.2) // 迁移20%的blocks

		// 执行迁移到最空闲的节点
		targetNode := h.findNodeByID(underloadedNodes[0].NodeId, nodes)
		if targetNode != nil && len(blocksToMigrate) > 0 {
			h.migrateBlocks(sourceNode, targetNode, blocksToMigrate)

			// 记录迁移历史
			record := MigrationRecord{
				RequestId:      h.requestCounter,
				SourceNode:     sourceNode.ID,
				TargetNode:     targetNode.ID,
				MigratedBlocks: blocksToMigrate,
				Reason:         fmt.Sprintf("Concentration ratio %.2f exceeded threshold %.2f",
					overloaded.ConcentrationRatio, h.MigrationThreshold),
			}
			h.migrationHistory = append(h.migrationHistory, record)

			fmt.Printf("🔄 [Migration] %s -> %s, migrated %d blocks (ratio: %.2f)\n",
				sourceNode.ID, targetNode.ID, len(blocksToMigrate), overloaded.ConcentrationRatio)
		}

		// 更新underloaded节点列表
		if len(underloadedNodes) > 1 {
			underloadedNodes = underloadedNodes[1:]
		}
	}
}

func (h *HotspotMigrationSelector) selectBlocksForMigration(node *PrefillNode, migrationRatio float64) []int {
	if len(node.CacheBlocks) == 0 {
		return nil
	}

	// 按访问频率排序，优先迁移低频blocks
	type blockFreq struct {
		hashID   int
		hitCount int
	}

	var blocks []blockFreq
	for hashID, block := range node.CacheBlocks {
		blocks = append(blocks, blockFreq{
			hashID:   hashID,
			hitCount: block.HitCount,
		})
	}

	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].hitCount < blocks[j].hitCount
	})

	// 选择要迁移的数量
	migrateCount := int(math.Max(1, float64(len(blocks))*migrationRatio))
	if migrateCount > len(blocks) {
		migrateCount = len(blocks)
	}

	var result []int
	for i := 0; i < migrateCount; i++ {
		result = append(result, blocks[i].hashID)
	}

	return result
}

func (h *HotspotMigrationSelector) migrateBlocks(sourceNode, targetNode *PrefillNode, blockIDs []int) {
	for _, hashID := range blockIDs {
		if block, exists := sourceNode.CacheBlocks[hashID]; exists {
			// 从源节点删除
			delete(sourceNode.CacheBlocks, hashID)

			// 添加到目标节点
			targetNode.CacheBlocks[hashID] = block

			// 检查目标节点容量，如果需要则触发淘汰
			if len(targetNode.CacheBlocks) > targetNode.MaxCacheSize {
				// 这里简单地删除一个随机block，实际中应该使用淘汰算法
				for id := range targetNode.CacheBlocks {
					delete(targetNode.CacheBlocks, id)
					break
				}
			}
		}
	}
}

func (h *HotspotMigrationSelector) findNodeByID(nodeID string, nodes []*PrefillNode) *PrefillNode {
	for _, node := range nodes {
		if node.ID == nodeID {
			return node
		}
	}
	return nil
}

func (h *HotspotMigrationSelector) GetName() string {
	return fmt.Sprintf("HotspotMigration(α=%.1f,β=%.1f,thresh=%.1f)",
		h.Alpha, h.Beta, h.MigrationThreshold)
}

func (h *HotspotMigrationSelector) PrintMigrationStats() {
	fmt.Printf("\n📊 热点迁移统计:\n")
	fmt.Printf("总迁移次数: %d\n", len(h.migrationHistory))

	if len(h.migrationHistory) > 0 {
		fmt.Printf("迁移历史:\n")
		for i, record := range h.migrationHistory {
			if i < 10 { // 只显示前10次迁移
				fmt.Printf("  #%d: %s->%s, %d blocks, 原因: %s\n",
					record.RequestId, record.SourceNode, record.TargetNode,
					len(record.MigratedBlocks), record.Reason)
			}
		}
		if len(h.migrationHistory) > 10 {
			fmt.Printf("  ... 还有 %d 次迁移\n", len(h.migrationHistory)-10)
		}
	}
}

// RunHotspotMigrationTest 运行热点迁移测试
func RunHotspotMigrationTest() {
	fmt.Println("\n============= 热点迁移机制测试 =============")

	// 创建带热点迁移的选择器
	migrationSelector := NewHotspotMigrationSelector(
		0.6,  // α: 缓存亲和性权重
		0.8,  // β: 负载均衡权重
		0.7,  // 迁移阈值: 当单节点占70%以上缓存时触发迁移
		0.1,  // 热点阈值: 访问频率超过10%认为是热点
	)

	// 创建测试节点
	nodes := []*PrefillNode{
		{ID: "node-0", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0), MaxCacheSize: 500},
		{ID: "node-1", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0), MaxCacheSize: 500},
		{ID: "node-2", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0), MaxCacheSize: 500},
		{ID: "node-3", CacheBlocks: make(map[int]*Block), RequestQueue: make([]*Request, 0), MaxCacheSize: 500},
	}

	// 加载请求数据
	requests, err := LoadRequests("mooncake_trace.jsonl")
	if err != nil {
		fmt.Printf("加载数据失败: %v\n", err)
		return
	}

	// 运行模拟（只处理前5000个请求以演示）
	totalHits := 0
	totalRequests := 0
	processCount := 5000
	if len(requests) < processCount {
		processCount = len(requests)
	}

	for i, request := range requests[:processCount] {
		selectedNode := migrationSelector.SelectNode(request, nodes)

		// 模拟请求处理和缓存更新
		hits := 0
		for _, hashID := range request.HashIDs {
			if _, exists := selectedNode.CacheBlocks[hashID]; exists {
				hits++
				selectedNode.CacheBlocks[hashID].HitCount++
			} else {
				// 添加新block
				selectedNode.CacheBlocks[hashID] = &Block{
					HashID:    hashID,
					HitCount:  1,
					AccessSeq: i,
					CreateSeq: i,
				}
			}
		}

		totalHits += hits
		totalRequests += len(request.HashIDs)

		// 简单的容量管理
		if len(selectedNode.CacheBlocks) > selectedNode.MaxCacheSize {
			// 随机删除一些blocks（简化的淘汰策略）
			count := 0
			for hashID := range selectedNode.CacheBlocks {
				delete(selectedNode.CacheBlocks, hashID)
				count++
				if count >= 50 { // 每次删除50个
					break
				}
			}
		}

		// 定期打印状态
		if (i+1)%1000 == 0 {
			fmt.Printf("处理进度: %d/%d, 当前命中率: %.2f%%\n",
				i+1, processCount, float64(totalHits)*100/float64(totalRequests))
		}
	}

	// 打印最终结果
	hitRate := float64(totalHits) * 100 / float64(totalRequests)
	fmt.Printf("\n🎯 带热点迁移的性能结果:\n")
	fmt.Printf("总请求数: %d\n", totalRequests)
	fmt.Printf("总命中数: %d\n", totalHits)
	fmt.Printf("命中率: %.2f%%\n", hitRate)

	// 打印节点分布
	fmt.Printf("\n📊 节点缓存分布:\n")
	totalBlocks := 0
	for _, node := range nodes {
		totalBlocks += len(node.CacheBlocks)
	}

	for _, node := range nodes {
		ratio := float64(len(node.CacheBlocks)) / float64(totalBlocks) * 100
		fmt.Printf("%s: %d blocks (%.1f%%)\n", node.ID, len(node.CacheBlocks), ratio)
	}

	// 打印迁移统计
	migrationSelector.PrintMigrationStats()
}