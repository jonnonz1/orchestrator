import { useEffect, useRef, useMemo } from 'react';
import { useApi } from '../hooks/useApi';
import { useWebSocket } from '../hooks/useWebSocket';
import type { Task, WSMessage } from '../types';

// Parsed representation of Claude Code stream-json events
interface ParsedEvent {
  type: 'thinking' | 'text' | 'tool_call' | 'tool_result' | 'system' | 'result' | 'raw';
  content: string;
  toolName?: string;
  toolInput?: string;
  isError?: boolean;
  cost?: number;
  model?: string;
}

function parseStreamMessages(messages: WSMessage[]): ParsedEvent[] {
  const events: ParsedEvent[] = [];

  for (const msg of messages) {
    if (msg.type === 'exit') {
      events.push({ type: 'system', content: `Process exited with code ${msg.data}` });
      continue;
    }
    if (msg.type === 'stderr') {
      if (msg.data.trim()) {
        events.push({ type: 'raw', content: msg.data, isError: true });
      }
      continue;
    }

    // Try to parse as JSON (stream-json format)
    let parsed: any;
    try {
      parsed = JSON.parse(msg.data);
    } catch {
      if (msg.data.trim()) {
        events.push({ type: 'raw', content: msg.data });
      }
      continue;
    }

    // Skip rate_limit_event and other noise
    if (parsed.type === 'rate_limit_event') continue;

    // System init
    if (parsed.type === 'system' && parsed.subtype === 'init') {
      events.push({
        type: 'system',
        content: `Session started — model: ${parsed.model}, Claude Code ${parsed.claude_code_version}`,
        model: parsed.model,
      });
      continue;
    }

    // Assistant messages
    if (parsed.type === 'assistant' && parsed.message?.content) {
      for (const block of parsed.message.content) {
        if (block.type === 'thinking') {
          // Skip thinking blocks with signatures, show actual thinking
          if (block.thinking && !block.signature) {
            events.push({ type: 'thinking', content: block.thinking });
          }
          continue;
        }
        if (block.type === 'text') {
          events.push({ type: 'text', content: block.text });
          continue;
        }
        if (block.type === 'tool_use') {
          let inputStr = '';
          if (block.input) {
            if (typeof block.input === 'string') {
              inputStr = block.input;
            } else if (block.input.command) {
              inputStr = block.input.command;
            } else if (block.input.file_path) {
              inputStr = block.input.file_path;
            } else {
              inputStr = JSON.stringify(block.input, null, 2);
            }
          }
          events.push({
            type: 'tool_call',
            content: inputStr,
            toolName: block.name,
            toolInput: inputStr,
          });
          continue;
        }
      }
      continue;
    }

    // Tool results (user messages with tool_result)
    if (parsed.type === 'user' && parsed.message?.content) {
      for (const block of parsed.message.content) {
        if (block.type === 'tool_result' && block.content) {
          const content = typeof block.content === 'string'
            ? block.content
            : JSON.stringify(block.content);
          // Truncate very long tool results
          const truncated = content.length > 2000
            ? content.slice(0, 2000) + '\n... (truncated)'
            : content;
          events.push({
            type: 'tool_result',
            content: truncated,
            isError: block.is_error,
          });
        }
      }
      continue;
    }

    // Final result
    if (parsed.type === 'result') {
      events.push({
        type: 'result',
        content: parsed.result || '',
        cost: parsed.total_cost_usd,
      });
      continue;
    }
  }

  return events;
}

function parseStoredOutput(output: string): ParsedEvent[] {
  const lines = output.split('\n').filter(l => l.trim());
  const messages: WSMessage[] = lines.map(l => ({
    type: 'stdout',
    data: l,
    timestamp: '',
  }));
  return parseStreamMessages(messages);
}

interface ResultFile {
  name: string;
  url: string;
  size: number;
}

export default function TaskDetail({ taskId, onBack }: { taskId: string; onBack: () => void }) {
  const { data: task } = useApi<Task>(`/tasks/${taskId}`, 2000);
  const { data: files } = useApi<ResultFile[]>(`/tasks/${taskId}/files`, 5000);
  const isRunning = task?.status === 'running' || task?.status === 'pending';
  const { messages } = useWebSocket(isRunning ? `/api/v1/tasks/${taskId}/stream` : null);
  const logRef = useRef<HTMLDivElement>(null);

  const parsedEvents = useMemo(() => {
    if (messages.length > 0) {
      return parseStreamMessages(messages);
    }
    if (task?.output) {
      return parseStoredOutput(task.output);
    }
    return [];
  }, [messages, task?.output]);

  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [parsedEvents]);

  if (!task) return <p className="text-gray-500">Loading...</p>;

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-4">
        <button onClick={onBack} className="text-gray-400 hover:text-white text-sm">&larr; Back</button>
        <h2 className="text-xl font-semibold">Task {task.id}</h2>
        <span className={`px-2 py-0.5 rounded text-xs font-medium ${statusColor(task.status)}`}>
          {task.status}
        </span>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <InfoCard label="Prompt" value={task.prompt} />
        <div className="grid grid-cols-2 gap-2">
          <InfoCard label="VM" value={task.vm_name || '-'} />
          <InfoCard label="Exit Code" value={task.exit_code !== undefined ? String(task.exit_code) : '-'} />
          <InfoCard label="Cost" value={task.cost_usd ? `$${task.cost_usd.toFixed(4)}` : '-'} />
          <InfoCard label="Duration" value={
            task.started_at && task.completed_at
              ? `${((new Date(task.completed_at).getTime() - new Date(task.started_at).getTime()) / 1000).toFixed(1)}s`
              : '-'
          } />
        </div>
        <div className="bg-gray-900 rounded-lg p-3 border border-gray-800">
          <p className="text-xs text-gray-500 mb-2">Result Files</p>
          {(files || []).length === 0 ? (
            <p className="text-sm text-gray-600">{isRunning ? 'Task in progress...' : 'No files'}</p>
          ) : (
            <div className="space-y-1">
              {(files || []).map((f) => (
                <a key={f.name} href={f.url} target="_blank" rel="noopener noreferrer"
                  className="flex items-center gap-2 text-sm text-orange-400 hover:text-orange-300 py-0.5">
                  <span>{isImage(f.name) ? '🖼' : '📄'}</span>
                  <span className="truncate">{f.name}</span>
                  <span className="text-xs text-gray-500 ml-auto">{formatSize(f.size)}</span>
                </a>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Image previews */}
      {(files || []).filter(f => isImage(f.name)).length > 0 && (
        <div className="bg-gray-900 rounded-lg border border-gray-800 p-4">
          <h3 className="text-sm font-medium text-gray-400 mb-3">Screenshots / Images</h3>
          <div className="grid grid-cols-2 gap-4">
            {(files || []).filter(f => isImage(f.name)).map((f) => (
              <a key={f.name} href={f.url} target="_blank" rel="noopener noreferrer">
                <img src={f.url} alt={f.name}
                  className="rounded border border-gray-700 w-full hover:border-orange-500 transition-colors" />
                <p className="text-xs text-gray-500 mt-1">{f.name}</p>
              </a>
            ))}
          </div>
        </div>
      )}

      <div className="bg-gray-900 rounded-lg border border-gray-800">
        <div className="px-4 py-2 border-b border-gray-800 flex items-center justify-between">
          <h3 className="text-sm font-medium text-gray-400">Output</h3>
          {isRunning && (
            <div className="flex items-center gap-2">
              <span className="w-2 h-2 bg-green-400 rounded-full animate-pulse" />
              <span className="text-xs text-green-400">Running</span>
            </div>
          )}
        </div>
        <div ref={logRef} className="p-4 max-h-[600px] overflow-y-auto space-y-3">
          {parsedEvents.length > 0 ? (
            parsedEvents.map((event, i) => <EventBlock key={i} event={event} />)
          ) : (
            <p className="text-gray-500 text-sm">Waiting for output...</p>
          )}
        </div>
      </div>

      {task.error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4">
          <h3 className="text-sm font-medium text-red-400 mb-1">Error</h3>
          <p className="text-red-300 text-sm">{task.error}</p>
        </div>
      )}
    </div>
  );
}

function EventBlock({ event }: { event: ParsedEvent }) {
  switch (event.type) {
    case 'system':
      return (
        <div className="text-xs text-gray-500 italic border-l-2 border-gray-700 pl-3 py-1">
          {event.content}
        </div>
      );

    case 'thinking':
      return (
        <div className="border-l-2 border-purple-800 pl-3 py-1">
          <div className="text-xs text-purple-400 font-medium mb-1">Thinking</div>
          <div className="text-sm text-purple-300/70 italic">{event.content}</div>
        </div>
      );

    case 'text':
      return (
        <div className="border-l-2 border-blue-800 pl-3 py-1">
          <div className="text-xs text-blue-400 font-medium mb-1">Claude</div>
          <div className="text-sm text-gray-200 whitespace-pre-wrap">{event.content}</div>
        </div>
      );

    case 'tool_call':
      return (
        <div className="border-l-2 border-orange-800 pl-3 py-1">
          <div className="text-xs text-orange-400 font-medium mb-1">
            Tool: {event.toolName}
          </div>
          <pre className="text-xs text-orange-200/80 bg-gray-800 rounded p-2 overflow-x-auto whitespace-pre-wrap">
            {event.content}
          </pre>
        </div>
      );

    case 'tool_result':
      return (
        <div className={`border-l-2 ${event.isError ? 'border-red-800' : 'border-gray-700'} pl-3 py-1`}>
          <div className={`text-xs font-medium mb-1 ${event.isError ? 'text-red-400' : 'text-gray-500'}`}>
            Result {event.isError ? '(error)' : ''}
          </div>
          <pre className="text-xs text-gray-400 bg-gray-800 rounded p-2 overflow-x-auto whitespace-pre-wrap max-h-48 overflow-y-auto">
            {event.content}
          </pre>
        </div>
      );

    case 'result':
      return (
        <div className="border-l-2 border-green-800 pl-3 py-2 bg-green-900/10 rounded-r">
          <div className="text-xs text-green-400 font-medium mb-1">
            Final Result {event.cost ? `($${event.cost.toFixed(4)})` : ''}
          </div>
          <div className="text-sm text-gray-200 whitespace-pre-wrap">{event.content}</div>
        </div>
      );

    case 'raw':
      return (
        <div className={`text-xs font-mono ${event.isError ? 'text-red-400' : 'text-gray-500'}`}>
          {event.content}
        </div>
      );

    default:
      return null;
  }
}

function InfoCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-gray-900 rounded-lg p-3 border border-gray-800">
      <p className="text-xs text-gray-500 mb-1">{label}</p>
      <p className="text-sm break-all">{value}</p>
    </div>
  );
}

function isImage(name: string): boolean {
  return /\.(png|jpg|jpeg|gif|webp|svg|bmp)$/i.test(name);
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

function statusColor(status: string): string {
  const colors: Record<string, string> = {
    running: 'bg-green-900 text-green-300',
    completed: 'bg-blue-900 text-blue-300',
    failed: 'bg-red-900 text-red-300',
    pending: 'bg-yellow-900 text-yellow-300',
  };
  return colors[status] || 'bg-gray-700 text-gray-400';
}
