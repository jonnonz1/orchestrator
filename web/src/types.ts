export interface VM {
  name: string;
  pid: number;
  ram_mb: number;
  vcpus: number;
  vsock_cid: number;
  tap_dev: string;
  tap_ip: string;
  guest_ip: string;
  subnet: string;
  host_iface: string;
  jail_id: string;
  jailer_base: string;
  state: string;
  launched_at: string;
}

export interface Task {
  id: string;
  status: string;
  prompt: string;
  vm_name: string;
  ram_mb: number;
  vcpus: number;
  output?: string;
  error?: string;
  exit_code?: number;
  result_files?: string[];
  cost_usd?: number;
  created_at: string;
  started_at?: string;
  completed_at?: string;
}

export interface Stats {
  total_vms: number;
  running_vms: number;
  total_tasks: number;
}

export interface ExecResult {
  exit_code: number;
  stdout?: string;
  stderr?: string;
}

export interface WSMessage {
  type: string;
  data: string;
  timestamp: string;
}
