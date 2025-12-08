package convo

type Step interface {
	Handle(ctx *ConvoContext) Step
	GetAvailableStep() AvailableSteps
}
