"""Orchestrator Python SDK.

A thin wrapper around the Orchestrator REST API. Use this if you're driving Orchestrator
from Python code and don't want to assemble `requests` calls by hand.

Example:

    from orchestrator import Client

    client = Client("http://127.0.0.1:8080", token="...")
    task = client.run_task("Take a screenshot of example.com", ram_mb=4096)
    for line in client.stream(task["id"]):
        print(line)
    result = client.wait(task["id"])
    for fname in client.list_files(task["id"]):
        content = client.get_file(task["id"], fname)
"""

from .client import Client, OrchestratorError

__all__ = ["Client", "OrchestratorError"]
__version__ = "0.1.0"
