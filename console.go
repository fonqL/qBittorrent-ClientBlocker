package main

import (
	"os"
	"net"
	"time"
	"strings"
	"strconv"
	"runtime"
)

type IPInfoStruct struct {
	TorrentUploaded map[string]int64
}
type PeerInfoStruct struct {
	Timestamp int64
	Port      map[int]bool
	Progress  float64
	Uploaded  int64
}
type BlockPeerInfoStruct struct {
	Timestamp int64
	Port      map[int]bool
}
type TorrrentMapStruct struct {
	TotalSize int64
	Peers  			 map[string]PeerStruct
}

var currentTimestamp int64 = 0
var lastCleanTimestamp int64 = 0
var lastIPCleanTimestamp int64 = 0
var lastPeerCleanTimestamp int64 = 0
var ipMap = make(map[string]IPInfoStruct)
var peerMap = make(map[string]PeerInfoStruct)
var blockPeerMap = make(map[string]BlockPeerInfoStruct)
var torrentMap = make(map[string]TorrrentMapStruct)

func AddIPInfo(clientIP string, torrentInfoHash string, clientUploaded int64) {
	if !config.IPUploadedCheck || (config.IPUpCheckIncrementMB <= 0 && config.IPUpCheckPerTorrentRatio <= 0) {
		return
	}
	var clientTorrentUploadedMap map[string]int64
	if info, exist := ipMap[clientIP]; !exist {
		clientTorrentUploadedMap = make(map[string]int64)
	} else {
		clientTorrentUploadedMap = info.TorrentUploaded
	}
	clientTorrentUploadedMap[torrentInfoHash] = clientUploaded
	ipMap[clientIP] = IPInfoStruct { TorrentUploaded: clientTorrentUploadedMap }
}
func AddPeerInfo(peerIP string, peerPort int, peerProgress float64, peerUploaded int64) {
	if config.MaxIPPortCount <= 0 {
		return
	}
	peerIP = strings.ToLower(peerIP)
	var peerPortMap map[int]bool
	if peer, exist := peerMap[peerIP]; !exist {
		peerPortMap = make(map[int]bool)
	} else {
		peerPortMap = peer.Port
	}
	peerPortMap[peerPort] = true
	peerMap[peerIP] = PeerInfoStruct { Timestamp: currentTimestamp, Port: peerPortMap, Progress: peerProgress, Uploaded: peerUploaded }
}
func AddTorrentPeers(torrentInfoHash string, torrentTotalSize int64, peers map[string]PeerStruct) {
	if !config.BanByRelativeProgressUploaded {
		return
	}
	torrentMap[torrentInfoHash] = TorrrentMapStruct {TotalSize: torrentTotalSize, Peers: peers}
}
func AddBlockPeer(peerIP string, peerPort int) {
	peerIP = strings.ToLower(peerIP)
	var blockPeerPortMap map[int]bool
	if blockPeer, exist := blockPeerMap[peerIP]; !exist {
		blockPeerPortMap = make(map[int]bool)
	} else {
		blockPeerPortMap = blockPeer.Port
	}
	blockPeerPortMap[peerPort] = true
	blockPeerMap[peerIP] = BlockPeerInfoStruct { Timestamp: currentTimestamp, Port: blockPeerPortMap }
}
func IsBlockedPeer(peerIP string, peerPort int, updateTimestamp bool) bool {
	if blockPeer, exist := blockPeerMap[peerIP]; exist {
		if useNewBanPeersMethod {
			if _, exist1 := blockPeer.Port[-1]; !exist1 {
				if _, exist2 := blockPeer.Port[peerPort]; !exist2 {
					return false
				}
			}
		}
		if updateTimestamp {
			blockPeer.Timestamp = currentTimestamp
		}

		return true
	}

	return false
}
func IsIPTooHighUploaded(ipInfo IPInfoStruct, lastIPInfo IPInfoStruct, torrents map[string]TorrentStruct) int64 {
	var totalUploaded int64 = 0
	for torrentInfoHash, torrentUploaded := range ipInfo.TorrentUploaded {
		if config.IPUpCheckPerTorrentRatio > 0 {
			if torrentInfo, exist := torrents[torrentInfoHash]; exist {
				if torrentUploaded > (torrentInfo.TotalSize * int64(config.IPUpCheckPerTorrentRatio)) {
					return (torrentUploaded / 1024 / 1024)
				}
			}
		}
		if config.IPUpCheckIncrementMB > 0 {
			if lastTorrentUploaded, exist := lastIPInfo.TorrentUploaded[torrentInfoHash]; !exist {
				totalUploaded += torrentUploaded
			} else {
				totalUploaded += (torrentUploaded - lastTorrentUploaded)
			}
		}
	}
	if config.IPUpCheckIncrementMB > 0 {
		var totalUploadedMB int64 = (totalUploaded / 1024 / 1024)
		if totalUploadedMB > int64(config.IPUpCheckIncrementMB) {
			return totalUploadedMB
		}
	}
	return 0
}
func IsProgressNotMatchUploaded(torrentTotalSize int64, clientProgress float64, clientUploaded int64) bool {
	if config.BanByProgressUploaded && torrentTotalSize > 0 && clientProgress >= 0 && clientUploaded > 0 {
		/*
		条件 1. 若客户端对 Peer 上传已大于等于 Torrnet 大小的 2%;
		条件 2. 但 Peer 报告进度乘以下载量再乘以一定防误判倍率, 却比客户端上传量还小;
		若满足以上条件, 则认为 Peer 是有问题的.
		e.g.:
		若 torrentTotalSize: 100GB, clientProgress: 1% (0.01), clientUploaded: 6GB, config.BanByPUStartPrecent: 2 (0.02), config.BanByPUAntiErrorRatio: 5;
		判断条件 1:
		torrentTotalSize * config.BanByPUStartPrecent = 100GB * 0.02 = 2GB, clientUploaded = 6GB >= 2GB
		满足此条件;
		判断条件 2:
		torrentTotalSize * clientProgress * config.BanByPUAntiErrorRatio = 100GB * 0.01 * 5 = 5GB, 5GB < clientUploaded = 6GB
		满足此条件;
		则该 Peer 将被封禁, 由于其报告进度为 1%, 算入 config.BanByPUAntiErrorRatio 滞后防误判倍率后为 5% (5GB), 但客户端实际却已上传 6GB.
		*/
		startUploaded := (float64(torrentTotalSize) * (float64(config.BanByPUStartPrecent) / 100))
		peerReportDownloaded := (float64(torrentTotalSize) * clientProgress)
		if (clientUploaded / 1024 / 1024) >= int64(config.BanByPUStartMB) && float64(clientUploaded) >= startUploaded && (peerReportDownloaded * float64(config.BanByPUAntiErrorRatio)) < float64(clientUploaded) {
			return true
		}
	}
	return false
}
func IsProgressNotMatchUploaded_Relative(torrentTotalSize int64, progress float64, lastProgress float64, uploaded int64, lastUploaded int64) float64 {
	// 与IsProgressNotMatchUploaded保持一致
	if config.BanByRelativeProgressUploaded && torrentTotalSize > 0 && (progress-lastProgress) >= 0 && (uploaded-lastUploaded) > 0 {
		
		// 若客户端对 Peer 上传已大于 0, 且相对上传量大于起始上传量, 则继续判断.
		relativeUploaded  := (float64(uploaded - lastUploaded) / 1024 / 1024)
		relativeDownloaded := (float64(torrentTotalSize) * (progress - lastProgress))

		if relativeUploaded > float64(config.BanByRelativePUStartMB) {
			// 若相对上传百分比大于起始百分比, 则继续判断.
			if relativeUploaded > (float64(torrentTotalSize) * (float64(config.BanByRelativePUStartPrecent) / 100)) {
				// 若相对上传百分比大于 Peer 报告进度乘以一定防误判倍率, 则认为 Peer 是有问题的.
				if relativeUploaded > (relativeDownloaded * float64(config.BanByRelativePUAntiErrorRatio)) {
					return relativeUploaded
				}
			}
		}
	}
	return 0
}
func ClearBlockPeer() int {
	cleanCount := 0
	if config.CleanInterval == 0 || (lastCleanTimestamp + int64(config.CleanInterval) < currentTimestamp) {
		for clientIP, clientInfo := range blockPeerMap {
			if currentTimestamp > (clientInfo.Timestamp + int64(config.BanTime)) {
				cleanCount++
				delete(blockPeerMap, clientIP)
			}
		}
		if cleanCount != 0 {
			lastCleanTimestamp = currentTimestamp
			Log("ClearBlockPeer", "已清理过期客户端: %d 个", true, cleanCount)
		}
	}
	return cleanCount
}
func CheckTorrent(torrentInfoHash string, torrentInfo TorrentStruct) (int, *TorrentPeersStruct) {
	if torrentInfoHash == "" {
		return -1, nil
	}
	if torrentInfo.NumLeechs <= 0 {
		return -2, nil
	}
	torrentPeers := FetchTorrentPeers(torrentInfoHash)
	if torrentPeers == nil {
		return -3, nil
	}
	return 0, torrentPeers
}
func CheckPeer(peer PeerStruct, lastpeer *PeerStruct, torrentInfoHash string, torrentTotalSize int64) int {
	hasPeerClient := (peer.Client != "" || peer.Peer_ID_Client != "")
	if (!config.IgnoreEmptyPeer && !hasPeerClient) || peer.IP == "" || CheckPrivateIP(peer.IP) {
		return -1
	}
	if IsBlockedPeer(peer.IP, peer.Port, true) {
		Log("Debug-CheckPeer_IgnorePeer (Blocked)", "%s:%d %s|%s", false, peer.IP, peer.Port, strconv.QuoteToASCII(peer.Peer_ID_Client), strconv.QuoteToASCII(peer.Client))
		/*
		if peer.Port == -2 {
			return 4
		}
		*/
		if peer.Port == -1 {
			return 3
		}
		return 2
	}
	if IsProgressNotMatchUploaded(torrentTotalSize, peer.Progress, peer.Uploaded) {
		Log("CheckPeer_AddBlockPeer (Bad-Progress_Uploaded)", "%s:%d %s|%s (TorrentInfoHash: %s, TorrentTotalSize: %.2f MB, Progress: %.2f%%, Uploaded: %.2f MB)", true, peer.IP, peer.Port, strconv.QuoteToASCII(peer.Peer_ID_Client), strconv.QuoteToASCII(peer.Client), torrentInfoHash, (float64(torrentTotalSize) / 1024 / 1024), (peer.Progress * 100), (float64(peer.Uploaded) / 1024 / 1024))
		AddBlockPeer(peer.IP, peer.Port)
		return 1
	}
	if lastpeer != nil {
		if uploadDuring := IsProgressNotMatchUploaded_Relative(torrentTotalSize, peer.Progress, lastpeer.Progress, peer.Uploaded, lastpeer.Uploaded); uploadDuring > 1e-6 { // 浮点数的比较
			Log("CheckAllPeer_AddBlockPeer (Bad-Relative_Progress_Uploaded)", "%s:%d (UploadDuring: %.2f MB)", true, peer.IP, peer.Port, uploadDuring)
			AddBlockPeer(peer.IP, peer.Port)
			return 1
		}
	}
	if hasPeerClient {
		for _, v := range blockListCompiled {
			if v == nil {
				continue
			}
			if (peer.Client != "" && v.MatchString(peer.Client)) || (peer.Peer_ID_Client != "" && v.MatchString(peer.Peer_ID_Client)) {
				Log("CheckPeer_AddBlockPeer (Bad-Client)", "%s:%d %s|%s (TorrentInfoHash: %s)", true, peer.IP, peer.Port, strconv.QuoteToASCII(peer.Peer_ID_Client), strconv.QuoteToASCII(peer.Client), torrentInfoHash)
				AddBlockPeer(peer.IP, peer.Port)
				return 1
			}
		}
	}
	ip := net.ParseIP(peer.IP)
	if ip == nil {
		Log("Debug-CheckPeer_AddBlockPeer (Bad-IP)", "%s:%d %s|%s (TorrentInfoHash: %s)", false, peer.IP, -1, strconv.QuoteToASCII(peer.Peer_ID_Client), strconv.QuoteToASCII(peer.Client), torrentInfoHash)
	} else {
		for _, v := range ipBlockListCompiled {
			if v == nil {
				continue
			}
			if v.Contains(ip) {
				Log("CheckPeer_AddBlockPeer (Bad-IP_List)", "%s:%d %s|%s (TorrentInfoHash: %s)", true, peer.IP, -1, strconv.QuoteToASCII(peer.Peer_ID_Client), strconv.QuoteToASCII(peer.Client), torrentInfoHash)
				AddBlockPeer(peer.IP, -1)
				return 3
			}
		}
		for _, v := range ipfilterCompiled {
			if v == nil {
				continue
			}
			if v.Contains(ip) {
				Log("CheckPeer_AddBlockPeer (Bad-IP_Filter)", "%s:%d %s|%s (TorrentInfoHash: %s)", true, peer.IP, -1, strconv.QuoteToASCII(peer.Peer_ID_Client), strconv.QuoteToASCII(peer.Client), torrentInfoHash)
				AddBlockPeer(peer.IP, -1)
				return 3
			}
		}
	}
	return 0
}
func CheckAllIP(lastIPMap map[string]IPInfoStruct, torrents map[string]TorrentStruct) int {
	if config.IPUploadedCheck && (config.IPUpCheckIncrementMB > 0 || config.IPUpCheckPerTorrentRatio > 0) && len(lastIPMap) > 0 && currentTimestamp > (lastIPCleanTimestamp + int64(config.IPUpCheckInterval)) {
		blockCount := 0
		for ip, ipInfo := range ipMap {
			if IsBlockedPeer(ip, -1, false) {
				continue
			}
			if lastIPInfo, exist := lastIPMap[ip]; exist {
				if uploadDuring := IsIPTooHighUploaded(ipInfo, lastIPInfo, torrents); uploadDuring > 0 {
					Log("CheckAllIP_AddBlockPeer (Too high uploaded)", "%s:%d (UploadDuring: %.2f MB)", true, ip, -1, uploadDuring)
					blockCount++
					AddBlockPeer(ip, -1)
				}
			}
		}
		lastIPCleanTimestamp = currentTimestamp
		ipMap = make(map[string]IPInfoStruct)
		return blockCount
	}
	return 0
}
func CheckAllPeer() int {
	if config.MaxIPPortCount > 0 && currentTimestamp > (lastPeerCleanTimestamp + int64(config.PeerMapCleanInterval)) {
		blockCount := 0
		peerMapLoop:
		for ip, peerInfo := range peerMap {
			if IsBlockedPeer(ip, -1, false) || len(peerInfo.Port) <= 0 {
				continue
			}
			for port := range peerInfo.Port {
				if IsBlockedPeer(ip, port, false) {
					continue peerMapLoop
				}
			}
			if len(peerInfo.Port) > int(config.MaxIPPortCount) {
				Log("CheckAllPeer_AddBlockPeer (Too many ports)", "%s:%d", true, ip, -1)
				blockCount++
				AddBlockPeer(ip, -1)
				continue
			}
		}
		lastPeerCleanTimestamp = currentTimestamp
		peerMap = make(map[string]PeerInfoStruct)
		return blockCount
	}
	return 0
}
func Task() {
	if config.QBURL == "" {
		Log("Task", "检测到 QBURL 为空, 可能是未配置且未能自动读取 qBittorrent 配置文件", false)
		return
	}

	metadata := FetchMaindata()
	if metadata == nil {
		return
	}

	cleanCount := ClearBlockPeer()
	blockCount := 0
	ipBlockCount := 0
	emptyHashCount := 0
	noLeechersCount := 0
	badTorrentInfoCount := 0
	badPeersCount := 0
	lastIPMap := ipMap

	lastTorrentMap := torrentMap
	torrentMap = make(map[string]TorrrentMapStruct)

	for torrentInfoHash, torrentInfo := range metadata.Torrents {
		torrentStatus, torrentPeers := CheckTorrent(torrentInfoHash, torrentInfo)
		if config.Debug_CheckTorrent {
			Log("Debug-CheckTorrent", "%s (Status: %d)", false, torrentInfoHash, torrentStatus)
		}
		switch torrentStatus {
			case -1:
				emptyHashCount++
			case -2:
				noLeechersCount++
			case -3:
				badTorrentInfoCount++
			case 0:
				for _, peer := range torrentPeers.Peers {
					var lastpeer *PeerStruct = nil
					if config.BanByRelativeProgressUploaded {
						if lastTorrent, exist := lastTorrentMap[torrentInfoHash]; exist {
							if lp, exist := lastTorrent.Peers[peer.IP]; exist {
								lastpeer = &lp
							}
						}
					}
					peerStatus := CheckPeer(peer, lastpeer, torrentInfoHash, torrentInfo.TotalSize)
					if config.Debug_CheckPeer {
						Log("Debug-CheckPeer", "%s:%d %s|%s (Status: %d)", false, peer.IP, peer.Port, strconv.QuoteToASCII(peer.Peer_ID_Client), strconv.QuoteToASCII(peer.Client), peerStatus)
					}
					switch peerStatus {
						case 3:
							ipBlockCount++
						case 1:
							blockCount++
						case -1:
							badPeersCount++
						case 0:
							AddIPInfo(peer.IP, torrentInfoHash, peer.Uploaded)
							AddPeerInfo(peer.IP, peer.Port, peer.Progress, peer.Uploaded)
					}
				}
				AddTorrentPeers(torrentInfoHash, torrentInfo.TotalSize, torrentPeers.Peers)
		}
		if config.SleepTime != 0 {
			time.Sleep(time.Duration(config.SleepTime) * time.Millisecond)
		}
	}

	currentIPBlockCount := CheckAllIP(lastIPMap, metadata.Torrents)
	ipBlockCount += currentIPBlockCount
	blockCount += CheckAllPeer()

	Log("Debug-Task_IgnoreEmptyHashCount", "%d", false, emptyHashCount)
	Log("Debug-Task_IgnoreNoLeechersCount", "%d", false, noLeechersCount)
	Log("Debug-Task_IgnoreBadTorrentInfoCount", "%d", false, badTorrentInfoCount)
	Log("Debug-Task_IgnoreBadPeersCount", "%d", false, badPeersCount)
	if cleanCount != 0 || blockCount != 0 {
		peersStr := GenBlockPeersStr()
		Log("Debug-Task_GenBlockPeersStr", "%s", false, peersStr)
		SubmitBlockPeer(peersStr)
		if config.IPUploadedCheck || len(ipBlockListCompiled) > 0 {
			Log("Task", "此次封禁客户端: %d 个, 当前封禁客户端: %d 个, 此次封禁 IP 地址: %d 个, 当前封禁 IP 地址: %d 个", true, blockCount, len(blockPeerMap), currentIPBlockCount, ipBlockCount)
		} else {
			Log("Task", "此次封禁客户端: %d 个, 当前封禁客户端: %d 个", true, blockCount, len(blockPeerMap))
		}
	}
}
func GC() {
	ipMapGCCount := (len(peerMap) - 23333333)
	peerMapGCCount := (len(peerMap) - 23333333)

	if ipMapGCCount > 0 {
		for ip, _ := range ipMap {
			ipMapGCCount--
			delete(ipMap, ip)
			if ipMapGCCount <= 0 {
				break
			}
		}
		runtime.GC()
		Log("GC", "触发垃圾回收 (ipMap)", true)
	}
	if peerMapGCCount > 0 {
		for ip, _ := range peerMap {
			peerMapGCCount--
			delete(peerMap, ip)
			if peerMapGCCount <= 0 {
				break
			}
		}
		runtime.GC()
		Log("GC", "触发垃圾回收 (peerMap)", true)
	}
}
func RunConsole() {
	RegFlag()
	ShowVersion()
	if shortFlag_ShowVersion || longFlag_ShowVersion {
		return
	}
	if !noChdir {
		dir, err := os.Getwd()
		if err == nil {
			if os.Chdir(dir) == nil {
				Log("RunConsole", "切换工作目录: %s", false, dir)
			} else {
				Log("RunConsole", "切换工作目录失败: %s", false, dir)
			}
		} else {
			Log("RunConsole", "切换工作目录失败, 将以当前工作目录运行: %s", false, err.Error())
		}
	}
	if config.StartDelay > 0 {
		Log("RunConsole", "启动延迟: %d 秒", false, config.StartDelay)
		time.Sleep(time.Duration(config.StartDelay) * time.Second)
	}
	if !LoadInitConfig(true) {
		Log("RunConsole", "认证失败", true)
		return
	}
	Log("RunConsole", "程序已启动", true)
	loopTicker := time.NewTicker(time.Duration(config.Interval) * time.Second)
	defer loopTicker.Stop()
	for ; true; <- loopTicker.C {
		currentTimestamp = time.Now().Unix()
		LoadInitConfig(false)
		Task()
		GC()
	}
}
