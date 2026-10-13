// Package quality 描述"上游给的这份文件能用到什么程度"。
//
// 单独一层的原因: 探测(netx)判断它、歌曲(song)承载它、调度(schedule)按它挑渠道、
// 发送(play)按它决定怎么投递 —— 四个包共用同一个刻度, 挂在谁家都别扭。
//
// 加一种新的坏链形态(试听片段、可疑小文件、容器不认识)时, 只需要在 netx 的判级函数里
// 加一条规则, 上下游都不必改。
package quality

// Grade 越大越可用。
type Grade int

const (
	// Unusable 根本不是媒体: 报错网页、JSON 正文、空正文、HTTP 错误码。
	Unusable Grade = iota
	// Degraded 文件是真的、发得出去, 但用户多半打不开 —— 酷我的加密格式(.mflac/.mgg)
	// 是眼下唯一一种。它是兜底, 不是失败: 挑不到更好的就发它。
	Degraded
	// Usable 正经音频。调度拿到这个等级就不必再问别的渠道了。
	Usable
)

func (g Grade) String() string {
	switch g {
	case Usable:
		return "可播"
	case Degraded:
		return "能发但打不开"
	default:
		return "不可用"
	}
}
