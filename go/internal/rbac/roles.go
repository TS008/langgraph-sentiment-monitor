package rbac

import "github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"

// Role maps agents to permission sets (deny-by-default).
type Role string

const (
	RoleSentinel       Role = "sentinel"
	RoleDiagnostician  Role = "diagnostician"
	RoleStrategist     Role = "strategist"
	RoleLegal          Role = "legal"
	RoleApprover       Role = "approver"
	RoleAction         Role = "action"
	RoleAuditor        Role = "auditor"
	RoleOrchestrator   Role = "orchestrator"
)

// Tool is an external or sensitive capability.
type Tool string

const (
	ToolCrawlPublic   Tool = "crawl_public"
	ToolLLMInfer      Tool = "llm_infer"
	ToolReadCasefile  Tool = "read_casefile"
	ToolWriteArtifact Tool = "write_artifact"
	ToolPublishBus    Tool = "publish_bus"
	ToolRequestApproval Tool = "request_approval"
	ToolExternalDispatch Tool = "external_dispatch"
	ToolAuditAppend   Tool = "audit_append"
)

var agentRole = map[agentbus.AgentID]Role{
	agentbus.AgentSentinel:      RoleSentinel,
	agentbus.AgentSentiment:     RoleSentinel,
	agentbus.AgentDiagnostician: RoleDiagnostician,
	agentbus.AgentStrategist:    RoleStrategist,
	agentbus.AgentLegal:         RoleLegal,
	agentbus.AgentGateway:       RoleApprover,
	agentbus.AgentAction:        RoleAction,
	agentbus.AgentFeedback:      RoleSentinel,
	agentbus.AgentStatus:        RoleAuditor,
	agentbus.AgentPostMortem:    RoleAuditor,
	agentbus.AgentOrchestrator:  RoleOrchestrator,
	agentbus.AgentHuman:         RoleApprover,
}

var roleTools = map[Role]map[Tool]bool{
	RoleOrchestrator: {
		ToolPublishBus: true, ToolReadCasefile: true, ToolAuditAppend: true,
	},
	RoleSentinel: {
		ToolCrawlPublic: true, ToolLLMInfer: true, ToolReadCasefile: true,
		ToolWriteArtifact: true, ToolPublishBus: true, ToolAuditAppend: true,
	},
	RoleDiagnostician: {
		ToolLLMInfer: true, ToolReadCasefile: true, ToolWriteArtifact: true,
		ToolPublishBus: true, ToolAuditAppend: true,
	},
	RoleStrategist: {
		ToolLLMInfer: true, ToolReadCasefile: true, ToolWriteArtifact: true,
		ToolPublishBus: true, ToolAuditAppend: true,
	},
	RoleLegal: {
		ToolLLMInfer: true, ToolReadCasefile: true, ToolWriteArtifact: true,
		ToolPublishBus: true, ToolAuditAppend: true,
	},
	RoleApprover: {
		ToolReadCasefile: true, ToolPublishBus: true, ToolRequestApproval: true,
		ToolAuditAppend: true,
	},
	RoleAction: {
		ToolReadCasefile: true, ToolPublishBus: true, ToolRequestApproval: true,
		ToolExternalDispatch: true, ToolAuditAppend: true,
	},
	RoleAuditor: {
		ToolReadCasefile: true, ToolPublishBus: true, ToolLLMInfer: true,
		ToolWriteArtifact: true, ToolAuditAppend: true,
	},
}

// RoleForAgent resolves the RBAC role for an agent id.
func RoleForAgent(a agentbus.AgentID) Role {
	if r, ok := agentRole[a]; ok {
		return r
	}
	return ""
}

// Allow returns whether the agent may use the tool (deny by default).
func Allow(agent agentbus.AgentID, tool Tool) bool {
	role := RoleForAgent(agent)
	if role == "" {
		return false
	}
	perms, ok := roleTools[role]
	if !ok {
		return false
	}
	return perms[tool]
}
