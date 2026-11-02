from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PitfallOutcome(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PITFALL_OUTCOME_UNSPECIFIED: _ClassVar[PitfallOutcome]
    PITFALL_OUTCOME_RECOGNIZED: _ClassVar[PitfallOutcome]
    PITFALL_OUTCOME_FELL: _ClassVar[PitfallOutcome]
    PITFALL_OUTCOME_PENDING: _ClassVar[PitfallOutcome]

class ActionKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ACTION_KIND_UNSPECIFIED: _ClassVar[ActionKind]
    ACTION_KIND_FOLLOW_AI: _ClassVar[ActionKind]
    ACTION_KIND_INDEPENDENT: _ClassVar[ActionKind]

class ChunkType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CHUNK_TYPE_UNSPECIFIED: _ClassVar[ChunkType]
    CHUNK_TYPE_DELTA: _ClassVar[ChunkType]
    CHUNK_TYPE_THINKING: _ClassVar[ChunkType]
    CHUNK_TYPE_DONE: _ClassVar[ChunkType]
    CHUNK_TYPE_ERROR: _ClassVar[ChunkType]
    CHUNK_TYPE_TOOL_CALL: _ClassVar[ChunkType]
    CHUNK_TYPE_STEP: _ClassVar[ChunkType]
PITFALL_OUTCOME_UNSPECIFIED: PitfallOutcome
PITFALL_OUTCOME_RECOGNIZED: PitfallOutcome
PITFALL_OUTCOME_FELL: PitfallOutcome
PITFALL_OUTCOME_PENDING: PitfallOutcome
ACTION_KIND_UNSPECIFIED: ActionKind
ACTION_KIND_FOLLOW_AI: ActionKind
ACTION_KIND_INDEPENDENT: ActionKind
CHUNK_TYPE_UNSPECIFIED: ChunkType
CHUNK_TYPE_DELTA: ChunkType
CHUNK_TYPE_THINKING: ChunkType
CHUNK_TYPE_DONE: ChunkType
CHUNK_TYPE_ERROR: ChunkType
CHUNK_TYPE_TOOL_CALL: ChunkType
CHUNK_TYPE_STEP: ChunkType

class CollabQuestion(_message.Message):
    __slots__ = ("text", "stage", "references_known_info", "follow_up")
    TEXT_FIELD_NUMBER: _ClassVar[int]
    STAGE_FIELD_NUMBER: _ClassVar[int]
    REFERENCES_KNOWN_INFO_FIELD_NUMBER: _ClassVar[int]
    FOLLOW_UP_FIELD_NUMBER: _ClassVar[int]
    text: str
    stage: str
    references_known_info: bool
    follow_up: bool
    def __init__(self, text: _Optional[str] = ..., stage: _Optional[str] = ..., references_known_info: _Optional[bool] = ..., follow_up: _Optional[bool] = ...) -> None: ...

class CollabPitfall(_message.Message):
    __slots__ = ("outcome",)
    OUTCOME_FIELD_NUMBER: _ClassVar[int]
    outcome: PitfallOutcome
    def __init__(self, outcome: _Optional[_Union[PitfallOutcome, str]] = ...) -> None: ...

class CollabAction(_message.Message):
    __slots__ = ("kind", "verified_before_follow")
    KIND_FIELD_NUMBER: _ClassVar[int]
    VERIFIED_BEFORE_FOLLOW_FIELD_NUMBER: _ClassVar[int]
    kind: ActionKind
    verified_before_follow: bool
    def __init__(self, kind: _Optional[_Union[ActionKind, str]] = ..., verified_before_follow: _Optional[bool] = ...) -> None: ...

class EvaluateCollabRequest(_message.Message):
    __slots__ = ("session_id", "questions", "pitfalls", "actions")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    QUESTIONS_FIELD_NUMBER: _ClassVar[int]
    PITFALLS_FIELD_NUMBER: _ClassVar[int]
    ACTIONS_FIELD_NUMBER: _ClassVar[int]
    session_id: int
    questions: _containers.RepeatedCompositeFieldContainer[CollabQuestion]
    pitfalls: _containers.RepeatedCompositeFieldContainer[CollabPitfall]
    actions: _containers.RepeatedCompositeFieldContainer[CollabAction]
    def __init__(self, session_id: _Optional[int] = ..., questions: _Optional[_Iterable[_Union[CollabQuestion, _Mapping]]] = ..., pitfalls: _Optional[_Iterable[_Union[CollabPitfall, _Mapping]]] = ..., actions: _Optional[_Iterable[_Union[CollabAction, _Mapping]]] = ...) -> None: ...

class EvaluateCollabResponse(_message.Message):
    __slots__ = ("available", "question_quality_score", "info_utilization_score", "judgment_score", "error_recognition_rate", "evidence_validation_rate", "pitfall_total", "pitfall_recognized", "follow_total", "follow_verified")
    AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    QUESTION_QUALITY_SCORE_FIELD_NUMBER: _ClassVar[int]
    INFO_UTILIZATION_SCORE_FIELD_NUMBER: _ClassVar[int]
    JUDGMENT_SCORE_FIELD_NUMBER: _ClassVar[int]
    ERROR_RECOGNITION_RATE_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_VALIDATION_RATE_FIELD_NUMBER: _ClassVar[int]
    PITFALL_TOTAL_FIELD_NUMBER: _ClassVar[int]
    PITFALL_RECOGNIZED_FIELD_NUMBER: _ClassVar[int]
    FOLLOW_TOTAL_FIELD_NUMBER: _ClassVar[int]
    FOLLOW_VERIFIED_FIELD_NUMBER: _ClassVar[int]
    available: bool
    question_quality_score: float
    info_utilization_score: float
    judgment_score: float
    error_recognition_rate: float
    evidence_validation_rate: float
    pitfall_total: int
    pitfall_recognized: int
    follow_total: int
    follow_verified: int
    def __init__(self, available: _Optional[bool] = ..., question_quality_score: _Optional[float] = ..., info_utilization_score: _Optional[float] = ..., judgment_score: _Optional[float] = ..., error_recognition_rate: _Optional[float] = ..., evidence_validation_rate: _Optional[float] = ..., pitfall_total: _Optional[int] = ..., pitfall_recognized: _Optional[int] = ..., follow_total: _Optional[int] = ..., follow_verified: _Optional[int] = ...) -> None: ...

class ToolDef(_message.Message):
    __slots__ = ("name", "description", "parameters_json", "requires_approval")
    NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_JSON_FIELD_NUMBER: _ClassVar[int]
    REQUIRES_APPROVAL_FIELD_NUMBER: _ClassVar[int]
    name: str
    description: str
    parameters_json: str
    requires_approval: bool
    def __init__(self, name: _Optional[str] = ..., description: _Optional[str] = ..., parameters_json: _Optional[str] = ..., requires_approval: _Optional[bool] = ...) -> None: ...

class ToolCall(_message.Message):
    __slots__ = ("id", "name", "arguments_json")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    ARGUMENTS_JSON_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    arguments_json: str
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., arguments_json: _Optional[str] = ...) -> None: ...

class ToolResult(_message.Message):
    __slots__ = ("call_id", "content_json", "is_error")
    CALL_ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_JSON_FIELD_NUMBER: _ClassVar[int]
    IS_ERROR_FIELD_NUMBER: _ClassVar[int]
    call_id: str
    content_json: str
    is_error: bool
    def __init__(self, call_id: _Optional[str] = ..., content_json: _Optional[str] = ..., is_error: _Optional[bool] = ...) -> None: ...

class StepEvent(_message.Message):
    __slots__ = ("step_id", "state", "tool", "display", "summary", "detail_json")
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    TOOL_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    DETAIL_JSON_FIELD_NUMBER: _ClassVar[int]
    step_id: str
    state: str
    tool: str
    display: str
    summary: str
    detail_json: str
    def __init__(self, step_id: _Optional[str] = ..., state: _Optional[str] = ..., tool: _Optional[str] = ..., display: _Optional[str] = ..., summary: _Optional[str] = ..., detail_json: _Optional[str] = ...) -> None: ...

class ChatChunk(_message.Message):
    __slots__ = ("type", "delta", "is_pitfall", "prompt_tokens", "completion_tokens", "cost", "error", "hint_level", "hint_remaining", "tool_calls", "step")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    DELTA_FIELD_NUMBER: _ClassVar[int]
    IS_PITFALL_FIELD_NUMBER: _ClassVar[int]
    PROMPT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COMPLETION_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    HINT_LEVEL_FIELD_NUMBER: _ClassVar[int]
    HINT_REMAINING_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    STEP_FIELD_NUMBER: _ClassVar[int]
    type: ChunkType
    delta: str
    is_pitfall: bool
    prompt_tokens: int
    completion_tokens: int
    cost: float
    error: str
    hint_level: int
    hint_remaining: int
    tool_calls: _containers.RepeatedCompositeFieldContainer[ToolCall]
    step: StepEvent
    def __init__(self, type: _Optional[_Union[ChunkType, str]] = ..., delta: _Optional[str] = ..., is_pitfall: _Optional[bool] = ..., prompt_tokens: _Optional[int] = ..., completion_tokens: _Optional[int] = ..., cost: _Optional[float] = ..., error: _Optional[str] = ..., hint_level: _Optional[int] = ..., hint_remaining: _Optional[int] = ..., tool_calls: _Optional[_Iterable[_Union[ToolCall, _Mapping]]] = ..., step: _Optional[_Union[StepEvent, _Mapping]] = ...) -> None: ...

class GenerateReviewRequest(_message.Message):
    __slots__ = ("session_id", "fault_type", "operation_timeline_json", "pitfall_records_json")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    FAULT_TYPE_FIELD_NUMBER: _ClassVar[int]
    OPERATION_TIMELINE_JSON_FIELD_NUMBER: _ClassVar[int]
    PITFALL_RECORDS_JSON_FIELD_NUMBER: _ClassVar[int]
    session_id: int
    fault_type: str
    operation_timeline_json: str
    pitfall_records_json: str
    def __init__(self, session_id: _Optional[int] = ..., fault_type: _Optional[str] = ..., operation_timeline_json: _Optional[str] = ..., pitfall_records_json: _Optional[str] = ...) -> None: ...

class GenerateReviewResponse(_message.Message):
    __slots__ = ("review_id", "sections_json")
    REVIEW_ID_FIELD_NUMBER: _ClassVar[int]
    SECTIONS_JSON_FIELD_NUMBER: _ClassVar[int]
    review_id: int
    sections_json: str
    def __init__(self, review_id: _Optional[int] = ..., sections_json: _Optional[str] = ...) -> None: ...

class ChatRequest(_message.Message):
    __slots__ = ("session_id", "conversation_id", "message", "agent_level", "history_json", "conversation_type", "pitfall_instruction", "pitfall_type", "fault_context_json", "hint_level", "hint_remaining", "tools", "tool_results")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    AGENT_LEVEL_FIELD_NUMBER: _ClassVar[int]
    HISTORY_JSON_FIELD_NUMBER: _ClassVar[int]
    CONVERSATION_TYPE_FIELD_NUMBER: _ClassVar[int]
    PITFALL_INSTRUCTION_FIELD_NUMBER: _ClassVar[int]
    PITFALL_TYPE_FIELD_NUMBER: _ClassVar[int]
    FAULT_CONTEXT_JSON_FIELD_NUMBER: _ClassVar[int]
    HINT_LEVEL_FIELD_NUMBER: _ClassVar[int]
    HINT_REMAINING_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    TOOL_RESULTS_FIELD_NUMBER: _ClassVar[int]
    session_id: int
    conversation_id: int
    message: str
    agent_level: str
    history_json: str
    conversation_type: str
    pitfall_instruction: bool
    pitfall_type: str
    fault_context_json: str
    hint_level: int
    hint_remaining: int
    tools: _containers.RepeatedCompositeFieldContainer[ToolDef]
    tool_results: _containers.RepeatedCompositeFieldContainer[ToolResult]
    def __init__(self, session_id: _Optional[int] = ..., conversation_id: _Optional[int] = ..., message: _Optional[str] = ..., agent_level: _Optional[str] = ..., history_json: _Optional[str] = ..., conversation_type: _Optional[str] = ..., pitfall_instruction: _Optional[bool] = ..., pitfall_type: _Optional[str] = ..., fault_context_json: _Optional[str] = ..., hint_level: _Optional[int] = ..., hint_remaining: _Optional[int] = ..., tools: _Optional[_Iterable[_Union[ToolDef, _Mapping]]] = ..., tool_results: _Optional[_Iterable[_Union[ToolResult, _Mapping]]] = ...) -> None: ...

class ChatResponse(_message.Message):
    __slots__ = ("reply", "is_pitfall")
    REPLY_FIELD_NUMBER: _ClassVar[int]
    IS_PITFALL_FIELD_NUMBER: _ClassVar[int]
    reply: str
    is_pitfall: bool
    def __init__(self, reply: _Optional[str] = ..., is_pitfall: _Optional[bool] = ...) -> None: ...

class ComparisonSummaryRequest(_message.Message):
    __slots__ = ("comparison_json",)
    COMPARISON_JSON_FIELD_NUMBER: _ClassVar[int]
    comparison_json: str
    def __init__(self, comparison_json: _Optional[str] = ...) -> None: ...

class ComparisonSummaryChunk(_message.Message):
    __slots__ = ("type", "delta", "prompt_tokens", "completion_tokens", "cost", "error")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    DELTA_FIELD_NUMBER: _ClassVar[int]
    PROMPT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COMPLETION_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    type: ChunkType
    delta: str
    prompt_tokens: int
    completion_tokens: int
    cost: float
    error: str
    def __init__(self, type: _Optional[_Union[ChunkType, str]] = ..., delta: _Optional[str] = ..., prompt_tokens: _Optional[int] = ..., completion_tokens: _Optional[int] = ..., cost: _Optional[float] = ..., error: _Optional[str] = ...) -> None: ...
