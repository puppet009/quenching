// 本文件是 ComparisonSummaryStreamer 端口的 ai-service 适配器：
// 编排层只认事件通道，本文件负责把对照数据 JSON 透传给 ai-service，
// 并在后台 goroutine 中把 ComparisonSummaryChunk 转成 StreamEvent。
// 与 ai_streamer.go 同构，但入参只有一份对照 JSON，不做任何内容解释或改写。
package service

import (
	"context"
	"io"

	"github.com/quenching/proto/aiv1"
)

// AIComparisonSummaryStreamer 把 *AIClient 适配为编排层的 ComparisonSummaryStreamer。
type AIComparisonSummaryStreamer struct {
	client *AIClient
}

func NewAIComparisonSummaryStreamer(client *AIClient) *AIComparisonSummaryStreamer {
	return &AIComparisonSummaryStreamer{client: client}
}

// 编译期保证实现 ComparisonSummaryStreamer。
var _ ComparisonSummaryStreamer = (*AIComparisonSummaryStreamer)(nil)

// StreamComparisonSummary 发起对照总结流式调用，在后台 goroutine 中把
// ComparisonSummaryChunk 转成 StreamEvent。返回的通道在流结束（io.EOF）、
// 出错或 ctx 取消后关闭。
func (s *AIComparisonSummaryStreamer) StreamComparisonSummary(
	ctx context.Context, comparisonJSON string,
) (<-chan StreamEvent, error) {
	stream, err := s.client.GenerateComparisonSummary(ctx, &aiv1.ComparisonSummaryRequest{
		ComparisonJson: comparisonJSON,
	})
	if err != nil {
		return nil, err
	}

	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		for {
			chunk, recvErr := stream.Recv()
			if recvErr == io.EOF {
				return
			}
			if recvErr != nil {
				select {
				case out <- StreamEvent{Type: StreamError, ErrorMsg: recvErr.Error()}:
				case <-ctx.Done():
				}
				return
			}
			ev := convertComparisonChunk(chunk)
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
			// 错误帧作为最后一个事件下发后结束。
			if ev.Type == StreamError {
				return
			}
		}
	}()

	return out, nil
}

// convertComparisonChunk 把 ComparisonSummaryChunk 映射为 StreamEvent。
// 对照总结只有正文流，无坑点/提示等附加字段。
func convertComparisonChunk(c *aiv1.ComparisonSummaryChunk) StreamEvent {
	ev := StreamEvent{
		PromptTokens:     int(c.GetPromptTokens()),
		CompletionTokens: int(c.GetCompletionTokens()),
		Cost:             c.GetCost(),
	}
	switch c.GetType() {
	case aiv1.ChunkType_CHUNK_TYPE_THINKING:
		ev.Type = StreamThinking
		ev.Delta = c.GetDelta()
	case aiv1.ChunkType_CHUNK_TYPE_DELTA:
		ev.Type = StreamDelta
		ev.Delta = c.GetDelta()
	case aiv1.ChunkType_CHUNK_TYPE_DONE:
		ev.Type = StreamDone
	case aiv1.ChunkType_CHUNK_TYPE_ERROR:
		ev.Type = StreamError
		ev.ErrorMsg = c.GetError()
	default:
		ev.Type = StreamDelta
		ev.Delta = c.GetDelta()
	}
	return ev
}
