"""Installed-Hermes settings inspection only; never instantiate an agent or submit a prompt."""
import json
import os
import socket
import sys
from pathlib import Path

# Prevent metadata imports from making network requests during this inspection.
# This is a check-only guard, NOT the eventual worker's execution boundary.
def no_connect(*args, **kwargs):
    raise RuntimeError("network disabled during Stage 1 settings inspection")

socket.socket.connect = no_connect
socket.socket.connect_ex = no_connect

from hermes_cli.config import load_config
from hermes_cli.config_defaults import DEFAULT_CONFIG
from hermes_cli import __version__
from tui_gateway import server
from toolsets import get_toolset
from agent.auxiliary_client import _discovery_chain_allowed

cfg = load_config()
expected = json.loads(os.environ["SPIKE_EXPECTED"])
checks = {
    "version": __version__ == expected["version"],
    "model": cfg["model"]["default"] == expected["model"],
    "provider": cfg["model"]["provider"] == "custom",
    "endpoint": cfg["model"]["base_url"] == expected["base_url"],
    "auto_continue_disabled": server._auto_continue_config()[0] is False,
    "fallback_chain_empty": server._load_fallback_model() == [],
    "toolsets_exact": server._load_enabled_toolsets() == expected["toolsets"],
    "max_iterations": server._cfg_max_turns(server._load_cfg(), 500) == 8,
    "compression_disabled": cfg["compression"]["enabled"] is False,
    "idle_compression_disabled": cfg["compression"]["idle_compact_after_seconds"] == 0,
    "memory_disabled": not cfg["memory"]["memory_enabled"] and not cfg["memory"]["user_profile_enabled"],
    "manual_approvals": cfg["approvals"]["mode"] == "manual",
    "title_disabled": not cfg["auxiliary"]["title_generation"]["enabled"] and not cfg["auxiliary"]["title_generation"]["model_upgrade_enabled"],
    "background_review_disabled": not cfg["auxiliary"]["background_review"]["enabled"],
    "mcp_empty": not cfg.get("mcp_servers"),
    "hooks_empty": not cfg.get("hooks"),
    "terminal_cwd": cfg["terminal"]["cwd"] == os.environ["SPIKE_WORKSPACE"],
}
aux_tasks = sorted(k for k, v in DEFAULT_CONFIG["auxiliary"].items() if isinstance(v, dict))
checks["all_auxiliary_routes_pinned"] = all(
    cfg["auxiliary"][task].get("provider") == "custom"
    and cfg["auxiliary"][task].get("model") == expected["model"]
    and cfg["auxiliary"][task].get("base_url") == expected["base_url"]
    and cfg["auxiliary"][task].get("fallback_chain") == []
    for task in aux_tasks
)
tools = sorted({tool for name in expected["toolsets"] for tool in get_toolset(name)["tools"]})
checks["no_delegation_tools"] = not any("delegate" in t or "subagent" in t for t in tools)
checks["auxiliary_cloud_discovery_disabled"] = _discovery_chain_allowed("custom", "compression") is False
model, runtime = server._resolve_agent_model_runtime(None, None)
checks["resolved_runtime"] = (model == expected["model"] and runtime.get("provider") == "custom"
                              and runtime.get("base_url", "").rstrip("/") == expected["base_url"].rstrip("/")
                              and runtime.get("api_mode") == "chat_completions")
Path(sys.argv[1]).write_text(json.dumps({"checks": checks, "auxiliary_tasks": aux_tasks, "tools": tools,
                                       "model_turns_started": 0}, indent=2) + "\n")
if not all(checks.values()):
    raise SystemExit(1)
