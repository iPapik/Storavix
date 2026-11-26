package telemetry

// LocateCommand is deliberately limited to identification LEDs, not a shell command.
type LocateCommand struct {
 ID string `json:"id"`
 Action string `json:"action"`
 Controller string `json:"controller"`
 ControllerSerial string `json:"controllerSerial,omitempty"`
 Source string `json:"source"`
 Slot string `json:"slot"`
 Serial string `json:"serial"`
}
type LocateResult struct {
 ID string `json:"id"`
 OK bool `json:"ok"`
 Message string `json:"message"`
}
type LocatePoll struct {
 AgentID string `json:"agentId"`
 Result *LocateResult `json:"result,omitempty"`
}
