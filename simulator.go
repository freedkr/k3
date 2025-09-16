package main

import (
	"bufio"
	"container/list"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"
)

// ============= 核心数据结构 =============

// Block 表示一个KV Cache块
type Block struct {
	HashID    int // 块的hash标识
	Size      int // 块大小（token数，默认512）
	HitCount  int // 命中次数
	AccessSeq int // 访问序号（替代LastAccess时间戳）
	CreateSeq int // 创建序号（替代CreateTime时间戳）
	RefCount  int // 引用计数（用于热点检测）
}

// Request 表示一个推理请求
type Request struct {
	Timestamp    int   `json:"timestamp"`     // 到达时间（毫秒）
	InputLength  int   `json:"input_length"`  // 输入token数
	OutputLength int   `json:"output_length"` // 输出token数
	HashIDs      []int `json:"hash_ids"`      // 块的hash ID列表
}

// PrefillNode 表示一个prefill节点
type PrefillNode struct {
	ID               string
	CacheBlocks      map[int]*Block    // 缓存的blocks
	MaxCacheSize     int               // 最大缓存块数
	MaxMemoryMB      int               // 最大内存（MB）
	UsedMemoryMB     float64           // 已使用内存
	TotalHits        int               // 总命中次数
	TotalMisses      int               // 总未命中次数
	EvictionAlgo     EvictionAlgorithm // 淘汰算法
	RequestQueue     []*Request        // 请求队列
	ProcessingTime   float64           // 处理时间（毫秒）
	NetworkBandwidth float64           // 网络带宽（GB/s）

	// 序号计数器（替代时间戳）
	seqCounter int // 全局序号计数器
}

// ============= 抽象接口定义 =============

// PrefillNodeSelector prefill节点选择器接口
type PrefillNodeSelector interface {
	// SelectNode 选择一个prefill节点处理请求
	SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode
	// GetName 获取选择器名称
	GetName() string
}

// EvictionAlgorithm 缓存淘汰算法接口
type EvictionAlgorithm interface {
	// Evict 选择要淘汰的block
	Evict(blocks map[int]*Block) int
	// UpdateOnAccess 访问block时更新状态
	UpdateOnAccess(block *Block)
	// OnAdd 添加新block时的回调（可选实现）
	OnAdd(blockID int)
	// GetName 获取算法名称
	GetName() string
}

// PrefillProcessor prefill处理器接口
type PrefillProcessor interface {
	// ProcessRequest 处理一个prefill请求
	ProcessRequest(request *Request, nodes []*PrefillNode) (*PrefillResult, error)
	// GetStatistics 获取统计信息
	GetStatistics() *SimulationStats
}

// ============= 结果和统计结构 =============

// PrefillResult prefill处理结果
type PrefillResult struct {
	SelectedNode    *PrefillNode
	CacheHits       int     // 命中的块数
	CacheMisses     int     // 未命中的块数
	ProcessedBlocks []int   // 处理的块ID列表
	TransferTime    float64 // 传输时间（毫秒）
	ProcessTime     float64 // 处理时间（毫秒）
}

// SimulationStats 模拟统计信息
type SimulationStats struct {
	TotalRequests   int
	TotalHits       int
	TotalMisses     int
	HitRate         float64
	AvgTransferTime float64
	AvgProcessTime  float64
	NodeStats       map[string]*NodeStatistics
}

// NodeStatistics 节点统计信息
type NodeStatistics struct {
	NodeID         string
	TotalRequests  int
	TotalHits      int
	TotalMisses    int
	HitRate        float64
	AvgMemoryUsage float64
	MaxMemoryUsage float64
	EvictedBlocks  int
}

// ============= 接口实现：随机选择器 =============

type RandomNodeSelector struct{}

func (r *RandomNodeSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}
	return nodes[rand.Intn(len(nodes))]
}

func (r *RandomNodeSelector) GetName() string {
	return "Random"
}

// ============= 接口实现：负载均衡选择器 =============

type LoadBalancedSelector struct{}

func (l *LoadBalancedSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	// 选择队列最短的节点
	minQueueNode := nodes[0]
	minQueueSize := len(nodes[0].RequestQueue)

	for _, node := range nodes[1:] {
		queueSize := len(node.RequestQueue)
		if queueSize < minQueueSize {
			minQueueSize = queueSize
			minQueueNode = node
		}
	}

	return minQueueNode
}

func (l *LoadBalancedSelector) GetName() string {
	return "LoadBalanced"
}

// ============= 接口实现：缓存感知选择器 =============

type CacheAwareSelector struct{}

func (c *CacheAwareSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	// 计算每个节点的缓存命中预期
	type nodeScore struct {
		node     *PrefillNode
		hitCount int
		load     float64
	}

	scores := make([]nodeScore, len(nodes))

	for i, node := range nodes {
		hitCount := 0
		for _, hashID := range request.HashIDs {
			if _, exists := node.CacheBlocks[hashID]; exists {
				hitCount++
			}
		}

		// 考虑负载因素 (修复: 使用更合理的负载计算)
		// 负载 = 队列长度 / 100 (标准化到0-1范围)
		load := float64(len(node.RequestQueue)) / 100.0
		scores[i] = nodeScore{
			node:     node,
			hitCount: hitCount,
			load:     load,
		}
	}

	// 选择得分最高的节点（命中多、负载低）
	// 修复: 确保负载权重有效，乘以系数增强负载影响
	bestNode := scores[0].node
	bestScore := float64(scores[0].hitCount) - scores[0].load*10.0

	for _, score := range scores[1:] {
		currentScore := float64(score.hitCount) - score.load*10.0
		if currentScore > bestScore {
			bestScore = currentScore
			bestNode = score.node
		}
	}

	return bestNode
}

// EnhancedCacheAwareSelector 增强版缓存感知选择器（包含α、β权重）
type EnhancedCacheAwareSelector struct {
	Alpha float64 // 缓存亲和性权重 (论文中的α)
	Beta  float64 // 负载均衡权重 (论文中的β)
}

func NewEnhancedCacheAwareSelector(alpha, beta float64) *EnhancedCacheAwareSelector {
	return &EnhancedCacheAwareSelector{
		Alpha: alpha,
		Beta:  beta,
	}
}

func (e *EnhancedCacheAwareSelector) SelectNode(request *Request, nodes []*PrefillNode) *PrefillNode {
	if len(nodes) == 0 {
		return nil
	}

	bestNode := nodes[0]
	bestScore := e.calculateScore(request, nodes[0], nodes)

	for _, node := range nodes[1:] {
		score := e.calculateScore(request, node, nodes)
		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}

	return bestNode
}

func (e *EnhancedCacheAwareSelector) calculateScore(request *Request, node *PrefillNode, allNodes []*PrefillNode) float64 {
	// 1. 计算缓存命中率 (归一化到[0,1])
	hitCount := 0
	for _, hashID := range request.HashIDs {
		if _, exists := node.CacheBlocks[hashID]; exists {
			hitCount++
		}
	}
	hitRatio := float64(hitCount) / float64(len(request.HashIDs))

	// 2. 计算归一化负载 (修复: 使用合理的基数)
	// 使用100作为标准化基数，而不是MaxCacheSize
	currentLoad := float64(len(node.RequestQueue)) / 100.0

	// 归一化：相对于所有节点的平均负载
	totalLoad := 0.0
	for _, n := range allNodes {
		totalLoad += float64(len(n.RequestQueue)) / 100.0
	}
	avgLoad := totalLoad / float64(len(allNodes))
	normalizedLoad := currentLoad
	if avgLoad > 0 {
		normalizedLoad = currentLoad / avgLoad
	}

	// 3. 应用α、β权重计算最终得分
	score := e.Alpha*hitRatio - e.Beta*normalizedLoad

	return score
}

func (e *EnhancedCacheAwareSelector) GetName() string {
	return fmt.Sprintf("EnhancedCacheAware(α=%.1f,β=%.1f)", e.Alpha, e.Beta)
}

func (c *CacheAwareSelector) GetName() string {
	return "CacheAware"
}

// ============= 接口实现：FIFO淘汰算法 =============

type FIFOEviction struct {
	insertOrder *list.List            // 维护插入顺序的队列
	orderNodes  map[int]*list.Element // blockID -> 队列中的节点
}

func NewFIFOEviction() *FIFOEviction {
	return &FIFOEviction{
		insertOrder: list.New(),
		orderNodes:  make(map[int]*list.Element),
	}
}

func (f *FIFOEviction) Evict(blocks map[int]*Block) int {
	if f.insertOrder.Len() == 0 {
		return -1
	}

	// 直接返回队列头部（最早插入的）
	front := f.insertOrder.Front()
	if front != nil {
		blockID := front.Value.(int)
		// 从队列和映射中移除
		f.insertOrder.Remove(front)
		delete(f.orderNodes, blockID)
		return blockID
	}
	return -1
}

func (f *FIFOEviction) UpdateOnAccess(block *Block) {
	// FIFO不关心访问时间，只关心插入顺序
	block.HitCount++
}

func (f *FIFOEviction) OnAdd(blockID int) {
	// 添加到队列尾部
	element := f.insertOrder.PushBack(blockID)
	f.orderNodes[blockID] = element
}

func (f *FIFOEviction) GetName() string {
	return "FIFO"
}

// ============= 接口实现：LRU淘汰算法 =============

type LRUEviction struct {
	accessOrder *list.List            // 维护访问顺序的双向链表（头部=最近，尾部=最久）
	orderNodes  map[int]*list.Element // blockID -> 链表中的节点
}

func NewLRUEviction() *LRUEviction {
	return &LRUEviction{
		accessOrder: list.New(),
		orderNodes:  make(map[int]*list.Element),
	}
}

func (l *LRUEviction) Evict(blocks map[int]*Block) int {
	if l.accessOrder.Len() == 0 {
		return -1
	}

	// 直接返回链表尾部（最久未使用的）
	back := l.accessOrder.Back()
	if back != nil {
		blockID := back.Value.(int)
		// 从链表和映射中移除
		l.accessOrder.Remove(back)
		delete(l.orderNodes, blockID)
		return blockID
	}
	return -1
}

func (l *LRUEviction) UpdateOnAccess(block *Block) {
	block.HitCount++
	blockID := block.HashID

	// 将访问的block移动到链表头部（最近使用）
	if element, exists := l.orderNodes[blockID]; exists {
		l.accessOrder.MoveToFront(element)
	}
}

func (l *LRUEviction) OnAdd(blockID int) {
	// 添加到链表头部（最近使用）
	element := l.accessOrder.PushFront(blockID)
	l.orderNodes[blockID] = element
}

func (l *LRUEviction) GetName() string {
	return "LRU"
}

// ============= 接口实现：LFU淘汰算法 =============

type LFUEviction struct {
	freqGroups map[int]*list.List    // 频率 -> 该频率的blocks列表
	blockFreq  map[int]int           // blockID -> 其当前频率
	blockNodes map[int]*list.Element // blockID -> 在频率链表中的位置
	minFreq    int                   // 当前最小频率
}

func NewLFUEviction() *LFUEviction {
	return &LFUEviction{
		freqGroups: make(map[int]*list.List),
		blockFreq:  make(map[int]int),
		blockNodes: make(map[int]*list.Element),
		minFreq:    1,
	}
}

func (l *LFUEviction) Evict(blocks map[int]*Block) int {
	if len(l.blockFreq) == 0 {
		return -1
	}

	// 找到最小频率组
	minFreqList := l.freqGroups[l.minFreq]
	if minFreqList == nil || minFreqList.Len() == 0 {
		// 重新计算最小频率
		l.updateMinFreq()
		minFreqList = l.freqGroups[l.minFreq]
	}

	if minFreqList != nil && minFreqList.Len() > 0 {
		// 选择该频率组中最早加入的（FIFO within same frequency）
		front := minFreqList.Front()
		if front != nil {
			blockID := front.Value.(int)
			l.removeBlock(blockID)
			return blockID
		}
	}

	return -1
}

func (l *LFUEviction) UpdateOnAccess(block *Block) {
	blockID := block.HashID
	block.HitCount++

	// 获取当前频率
	oldFreq, exists := l.blockFreq[blockID]
	if !exists {
		// 新block，初始频率为1
		oldFreq = 0
	}

	newFreq := oldFreq + 1

	// 从旧频率组移除
	if oldFreq > 0 {
		l.removeFromFreqGroup(blockID, oldFreq)
	}

	// 添加到新频率组
	l.addToFreqGroup(blockID, newFreq)
	l.blockFreq[blockID] = newFreq

	// 更新最小频率
	if oldFreq == l.minFreq && (l.freqGroups[oldFreq] == nil || l.freqGroups[oldFreq].Len() == 0) {
		l.minFreq = newFreq
	}
}

func (l *LFUEviction) OnAdd(blockID int) {
	// 新block初始频率为1
	l.addToFreqGroup(blockID, 1)
	l.blockFreq[blockID] = 1
	l.minFreq = 1
}

// 辅助方法
func (l *LFUEviction) removeBlock(blockID int) {
	freq := l.blockFreq[blockID]
	l.removeFromFreqGroup(blockID, freq)
	delete(l.blockFreq, blockID)
}

func (l *LFUEviction) addToFreqGroup(blockID int, freq int) {
	if l.freqGroups[freq] == nil {
		l.freqGroups[freq] = list.New()
	}
	element := l.freqGroups[freq].PushBack(blockID)
	l.blockNodes[blockID] = element
}

func (l *LFUEviction) removeFromFreqGroup(blockID int, freq int) {
	if element, exists := l.blockNodes[blockID]; exists {
		l.freqGroups[freq].Remove(element)
		delete(l.blockNodes, blockID)
	}
}

func (l *LFUEviction) updateMinFreq() {
	l.minFreq++
	for l.freqGroups[l.minFreq] == nil || l.freqGroups[l.minFreq].Len() == 0 {
		l.minFreq++
		if l.minFreq > 1000 { // 防止无限循环
			l.minFreq = 1
			break
		}
	}
}

func (l *LFUEviction) GetName() string {
	return "LFU"
}

// ============= 基础Prefill处理器实现 =============

type BasicPrefillProcessor struct {
	selector     PrefillNodeSelector
	stats        *SimulationStats
	nodeStatsMap map[string]*NodeStatistics
}

func NewBasicPrefillProcessor(selector PrefillNodeSelector) *BasicPrefillProcessor {
	return &BasicPrefillProcessor{
		selector: selector,
		stats: &SimulationStats{
			NodeStats: make(map[string]*NodeStatistics),
		},
		nodeStatsMap: make(map[string]*NodeStatistics),
	}
}

func (p *BasicPrefillProcessor) ProcessRequest(request *Request, nodes []*PrefillNode) (*PrefillResult, error) {
	// 1. 选择节点
	selectedNode := p.selector.SelectNode(request, nodes)
	if selectedNode == nil {
		return nil, fmt.Errorf("no available node")
	}

	// 添加请求到队列 (修复: RequestQueue之前从未更新)
	selectedNode.RequestQueue = append(selectedNode.RequestQueue, request)

	// 保持队列长度合理，模拟请求完成后的清理
	// 只保留最近的100个请求用于负载计算
	if len(selectedNode.RequestQueue) > 100 {
		selectedNode.RequestQueue = selectedNode.RequestQueue[len(selectedNode.RequestQueue)-100:]
	}

	// 初始化节点统计
	if _, exists := p.nodeStatsMap[selectedNode.ID]; !exists {
		p.nodeStatsMap[selectedNode.ID] = &NodeStatistics{
			NodeID: selectedNode.ID,
		}
	}
	nodeStats := p.nodeStatsMap[selectedNode.ID]

	result := &PrefillResult{
		SelectedNode:    selectedNode,
		ProcessedBlocks: request.HashIDs,
	}

	// 2. 处理每个block
	blockSize := 512.0                                 // 每个block的token数
	blockMemoryMB := blockSize * 2 * 4 / (1024 * 1024) // 假设每个token占用2*4字节（KV各4字节）

	for _, hashID := range request.HashIDs {
		if block, exists := selectedNode.CacheBlocks[hashID]; exists {
			// Cache命中
			result.CacheHits++
			selectedNode.TotalHits++
			selectedNode.EvictionAlgo.UpdateOnAccess(block)
		} else {
			// Cache未命中，需要添加
			result.CacheMisses++
			selectedNode.TotalMisses++

			// 检查内存容量
			requiredMemory := blockMemoryMB
			availableMemory := float64(selectedNode.MaxMemoryMB) - selectedNode.UsedMemoryMB

			// 如果内存不足，执行淘汰
			for availableMemory < requiredMemory && len(selectedNode.CacheBlocks) > 0 {
				evictID := selectedNode.EvictionAlgo.Evict(selectedNode.CacheBlocks)
				if evictID == -1 {
					break
				}
				delete(selectedNode.CacheBlocks, evictID)
				selectedNode.UsedMemoryMB -= blockMemoryMB
				nodeStats.EvictedBlocks++
				availableMemory = float64(selectedNode.MaxMemoryMB) - selectedNode.UsedMemoryMB
			}

			// 添加新block
			selectedNode.seqCounter++ // 递增序号计数器
			selectedNode.CacheBlocks[hashID] = &Block{
				HashID:    hashID,
				Size:      512,
				HitCount:  1,
				AccessSeq: selectedNode.seqCounter,
				CreateSeq: selectedNode.seqCounter,
			}
			selectedNode.UsedMemoryMB += blockMemoryMB
			selectedNode.EvictionAlgo.OnAdd(hashID) // 通知淘汰算法
		}
	}

	// 更新统计
	p.stats.TotalRequests++
	p.stats.TotalHits += result.CacheHits
	p.stats.TotalMisses += result.CacheMisses

	nodeStats.TotalRequests++
	nodeStats.TotalHits += result.CacheHits
	nodeStats.TotalMisses += result.CacheMisses

	// 计算处理时间
	result.ProcessTime = float64(request.InputLength) * 0.01 // 简化计算
	result.TransferTime = float64(result.CacheMisses) * blockMemoryMB / selectedNode.NetworkBandwidth

	return result, nil
}

func (p *BasicPrefillProcessor) GetStatistics() *SimulationStats {
	if p.stats.TotalRequests > 0 {
		p.stats.HitRate = float64(p.stats.TotalHits) / float64(p.stats.TotalHits+p.stats.TotalMisses)
	}

	// 计算每个节点的统计
	for nodeID, nodeStats := range p.nodeStatsMap {
		if nodeStats.TotalHits+nodeStats.TotalMisses > 0 {
			nodeStats.HitRate = float64(nodeStats.TotalHits) / float64(nodeStats.TotalHits+nodeStats.TotalMisses)
		}
		p.stats.NodeStats[nodeID] = nodeStats
	}

	return p.stats
}

// ============= 数据加载函数 =============

func LoadRequests(filename string) ([]*Request, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var requests []*Request
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var rawData map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &rawData); err != nil {
			continue
		}

		// 解析数据
		request := &Request{
			Timestamp:    int(rawData["timestamp"].(float64)),
			InputLength:  int(rawData["input_length"].(float64)),
			OutputLength: int(rawData["output_length"].(float64)),
		}

		// 解析hash_ids
		hashIDsRaw := rawData["hash_ids"].([]interface{})
		request.HashIDs = make([]int, len(hashIDsRaw))
		for i, id := range hashIDsRaw {
			request.HashIDs[i] = int(id.(float64))
		}

		requests = append(requests, request)
	}

	return requests, scanner.Err()
}

// ============= 模拟器主类 =============

type Simulator struct {
	nodes        []*PrefillNode
	processor    PrefillProcessor
	requests     []*Request
	selectorName string
}

func NewSimulator(nodeCount int, cacheSize int, selector PrefillNodeSelector, evictionAlgo func() EvictionAlgorithm) *Simulator {
	nodes := make([]*PrefillNode, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes[i] = &PrefillNode{
			ID:               fmt.Sprintf("node-%d", i),
			CacheBlocks:      make(map[int]*Block),
			MaxCacheSize:     cacheSize,
			MaxMemoryMB:      2,    // 减小到2MB以确保淘汰
			NetworkBandwidth: 10.0, // 10GB/s
			EvictionAlgo:     evictionAlgo(),
			seqCounter:       0, // 初始化序号计数器
		}
	}

	return &Simulator{
		nodes:        nodes,
		processor:    NewBasicPrefillProcessor(selector),
		selectorName: selector.GetName(),
	}
}

func (s *Simulator) LoadData(filename string) error {
	requests, err := LoadRequests(filename)
	if err != nil {
		return err
	}
	s.requests = requests
	return nil
}

func (s *Simulator) Run() *SimulationStats {
	fmt.Printf("\n开始模拟: 选择器=%s, 节点数=%d\n", s.selectorName, len(s.nodes))
	fmt.Println("处理中...")

	startTime := time.Now()
	processedCount := 0

	for i, request := range s.requests {
		_, err := s.processor.ProcessRequest(request, s.nodes)
		if err != nil {
			fmt.Printf("处理请求 %d 失败: %v\n", i, err)
			continue
		}

		processedCount++
		if processedCount%1000 == 0 {
			fmt.Printf("已处理 %d/%d 请求...\n", processedCount, len(s.requests))
		}
	}

	elapsed := time.Since(startTime)
	stats := s.processor.GetStatistics()

	fmt.Printf("\n模拟完成！耗时: %.2f秒\n", elapsed.Seconds())
	fmt.Printf("处理请求数: %d/%d\n", processedCount, len(s.requests))

	return stats
}

// ============= 结果输出函数 =============

func PrintStatistics(stats *SimulationStats, title string) {
	fmt.Printf("\n========== %s ==========\n", title)
	fmt.Printf("总请求数: %d\n", stats.TotalRequests)
	fmt.Printf("总命中数: %d\n", stats.TotalHits)
	fmt.Printf("总未命中数: %d\n", stats.TotalMisses)
	fmt.Printf("总体命中率: %.2f%%\n", stats.HitRate*100)

	if len(stats.NodeStats) > 0 {
		fmt.Println("\n节点统计:")
		for nodeID, nodeStats := range stats.NodeStats {
			fmt.Printf("  %s: 请求=%d, 命中率=%.2f%%, 淘汰块=%d\n",
				nodeID, nodeStats.TotalRequests, nodeStats.HitRate*100, nodeStats.EvictedBlocks)
		}
	}
}

// ============= 主函数 =============

func RunSimulation() {
	// 模拟配置
	nodeCount := 4
	cacheSize := 500 // 进一步减小到500以确保触发淘汰
	dataFile := "mooncake_trace.jsonl"

	// 测试不同的选择器和淘汰算法组合
	selectors := []PrefillNodeSelector{
		&RandomNodeSelector{},
		&LoadBalancedSelector{},
		&CacheAwareSelector{},
		// 测试不同的α、β权重组合
		NewEnhancedCacheAwareSelector(0.6, 0.8), // 论文推荐配置
		NewEnhancedCacheAwareSelector(0.4, 1.0), // 强化负载均衡
		NewEnhancedCacheAwareSelector(0.8, 0.6), // 强化缓存感知
		// 测试热点迁移机制 (暂时注释，需要引入hotspot_migration.go)
		// NewHotspotMigrationSelector(0.6, 0.8, 0.7, 0.1), // 带迁移的缓存感知
	}

	evictionAlgos := []func() EvictionAlgorithm{
		func() EvictionAlgorithm { return NewFIFOEviction() },
		func() EvictionAlgorithm { return NewLRUEviction() },
		func() EvictionAlgorithm { return NewLFUEviction() },
	}

	results := make(map[string]*SimulationStats)

	// 运行所有组合
	for _, selector := range selectors {
		for _, evictionAlgo := range evictionAlgos {
			sim := NewSimulator(nodeCount, cacheSize, selector, evictionAlgo)

			if err := sim.LoadData(dataFile); err != nil {
				fmt.Printf("加载数据失败: %v\n", err)
				continue
			}

			evictionName := evictionAlgo().GetName()
			configName := fmt.Sprintf("%s + %s", selector.GetName(), evictionName)

			stats := sim.Run()
			results[configName] = stats
			PrintStatistics(stats, configName)
		}
	}

	// 找出最佳配置
	fmt.Println("\n========== 性能对比 ==========")
	bestConfig := ""
	bestHitRate := 0.0

	for config, stats := range results {
		fmt.Printf("%-30s: 命中率=%.2f%%\n", config, stats.HitRate*100)
		if stats.HitRate > bestHitRate {
			bestHitRate = stats.HitRate
			bestConfig = config
		}
	}

	fmt.Printf("\n最佳配置: %s (命中率=%.2f%%)\n", bestConfig, bestHitRate*100)
	// RunTraceAnalysis()
	// // 其他优化版本暂时注释掉，待Block结构统一后启用
	// RunOptimizedSimulation()
	// RunDeepOptimizedSimulation()
	// RunLongestPrefixTest()
}
