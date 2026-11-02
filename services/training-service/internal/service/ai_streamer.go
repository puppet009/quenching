// 本文件是 ChatStreamer 端口的 ai-service 适配器（原 internal/ai 包，g6 批次3 并入 service）：
// 编排层只认事件通道，本文件负责 gRPC 连接、proto 转换与后台泵流，编排层不直接接触 grpc/proto。
package service

import (
	"context"
	"encoding/json"
	"io"

	"github.com/quenching/proto/aiv1"
)

// AIChatStreamer 把 *AIClient 适配为编排层的 ChatStreamer。
type AIChatStreamer struct {
	client *AIClient
}

func NewAIChatStreamer(client *AIClient) *AIChatStreamer { return &AIChatStreamer{client: client} }

// 编译期保证实现 ChatStreamer。
var _ ChatStreamer = (*AIChatStreamer)(nil)

// StreamChat 发起流式调用并在后台 goroutine 中把 ChatChunk 转成 StreamEvent。
// 返回的通道在流结束（io.EOF）、出错或 ctx 取消后关闭。
func (s *AIChatStreamer) StreamChat(ctx context.Context, in ChatStreamInput) (<-chan StreamEvent, error) {
	historyJSON, err := json.Marshal(in.History)
	if err != nil {
		return nil, err
	}

	req := &aiv1.ChatRequest{
		SessionId:          in.SessionID,
		ConversationId:     in.ConversationID,
		Message:            in.Message,
		AgentLevel:         in.AgentLevel,
		MemoryContextJson:  in.MemoryContextJSON,
		HistoryJson:        string(historyJSON),
		ConversationType:   in.ConversationType,
		PitfallInstruction: in.PitfallInstruction,
		PitfallType:        in.PitfallType,
		// T3-4/T3-5：脱敏故障上下文与渐进式提示级别/剩余次数原样透传，编排层不解释内容。
		FaultContextJson: in.FaultContextJSON,
		HintLevel:        int32(in.HintLevel),
		HintRemaining:    int32(in.HintRemaining),
		// Agent 工具调用：工具白名单与上一轮结果。
		Tools:       toProtoTools(in.Tools),
		ToolResults: toProtoToolResults(in.ToolResults),
	}

	stream, err := s.client.ChatStream(ctx, req)
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
			ev := convertChunk(chunk)
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

func convertChunk(c *aiv1.ChatChunk) StreamEvent {
	ev := StreamEvent{
		IsPitfall:        c.GetIsPitfall(),
		PromptTokens:     int(c.GetPromptTokens()),
		CompletionTokens: int(c.GetCompletionTokens()),
		Cost:             c.GetCost(),
		// T3-5：提示级别与剩余次数由 ai-service 原样回传，编排层负责计数与配额。
		HintLevel:     int(c.GetHintLevel()),
		HintRemaining: int(c.GetHintRemaining()),
		// Agent：工具调用与步骤事件。
		ToolCalls: fromProtoToolCalls(c.GetToolCalls()),
	}
	if st := c.GetStep(); st != nil {
		ev.Step = &StepEvent{
			StepID:     st.GetStepId(),
			State:      st.GetState(),
			Tool:       st.GetTool(),
			Display:    st.GetDisplay(),
			Summary:    st.GetSummary(),
			DetailJSON: st.GetDetailJson(),
		}
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
	case aiv1.ChunkType_CHUNK_TYPE_TOOL_CALL:
		ev.Type = StreamToolCall
	case aiv1.ChunkType_CHUNK_TYPE_STEP:
		ev.Type = StreamStep
	default:
		ev.Type = StreamDelta
		ev.Delta = c.GetDelta()
	}
	return ev
}

// toProtoTools 内部工具定义 -> proto。
func toProtoTools(in []ToolDef) []*aiv1.ToolDef {
	if len(in) == 0 {
		return nil
	}
	out := make([]*aiv1.ToolDef, 0, len(in))
	for _, t := range in {
		out = append(out, &aiv1.ToolDef{
			Name:             t.Name,
			Description:      t.Description,
			ParametersJson:   t.ParametersJSON,
			RequiresApproval: t.RequiresApproval,
		})
	}
	return out
}

// toProtoToolResults 内部工具结果 -> proto。
func toProtoToolResults(in []ToolResult) []*aiv1.ToolResult {
	if len(in) == 0 {
		return nil
	}
	out := make([]*aiv1.ToolResult, 0, len(in))
	for _, r := range in {
		out = append(out, &aiv1.ToolResult{
			CallId:      r.CallID,
			ContentJson: r.ContentJSON,
			IsError:     r.IsError,
		})
	}
	return out
}

// fromProtoToolCalls proto 工具调用 -> 内部。
func fromProtoToolCalls(in []*aiv1.ToolCall) []ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(in))
	for _, c := range in {
		out = append(out, ToolCall{
			ID:            c.GetId(),
			Name:          c.GetName(),
			ArgumentsJSON: c.GetArgumentsJson(),
		})
	}
	return out
}
