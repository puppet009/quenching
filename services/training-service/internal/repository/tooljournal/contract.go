package tooljournal

import oe "github.com/quenching/proto/operationevidence"

type Evidence = oe.Evidence
type Text = oe.Text
type Page = oe.Page
type Store = oe.Store
type AuditSeal = oe.AuditSeal
type Delivery = oe.Delivery

const TextLimit = oe.TextLimit

var ErrConflict = oe.ErrConflict
var ErrClosed = oe.ErrClosed
var ErrNotFound = oe.ErrNotFound
var Validate = oe.Validate
var Sanitize = oe.Sanitize
var RequestHash = oe.RequestHash
var ValidateTransition = oe.ValidateTransition
var RedactText = oe.RedactText
var Next = oe.Next
var EvaluateSeal = oe.EvaluateSeal
var Deliver = oe.Deliver

type Target = oe.Target
type Coverage = oe.Coverage
type Fragment = oe.Fragment

var EmptyCoverage = oe.EmptyCoverage
var CaptureText = oe.CaptureText
var ReadFragment = oe.ReadFragment
var Terminal = oe.Terminal
