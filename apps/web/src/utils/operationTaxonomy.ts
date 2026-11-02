// operationTaxonomy.ts 操作语义字典（复盘时间线的数据源）。
//
// 把"界面动作"与"终端命令"归一为 session_operations 的
// operation_type / operation_category，取值与后端
// services/training-service/internal/model/operation.go 及
// score_cost.go 的代价表同名，改名必须两端同步。
import type { OperationInput } from "../services/session";

/** 操作大类：观察 / 分析 / 动手 / 求助 AI（后端据此推断排障阶段）。 */
export type OperationCategory = "observe" | "analyze" | "action" | "ai_assist";

/** 命令分类结果。 */
export interface CommandClass {
  operationType: string;
  category: OperationCategory;
  /** >0 计入危险操作数（后端 dangerous = penalty>0 OR result='failed'）。 */
  penalty: number;
  /** 命令涉及的对象（服务名 / Pod / 文件路径），用于复盘"绕路"判定。 */
  target: string;
}

/** 读取类程序：只看不动，归 observe。 */
const READ_ONLY = new Set([
  "ps", "pgrep", "top", "htop", "vmstat", "free", "df", "du", "ls", "ll", "cd", "pwd",
  "find", "locate", "grep", "egrep", "rg", "cat", "head", "tail", "less", "more", "stat",
  "file", "env", "printenv", "export", "echo", "whoami", "id", "history", "uptime", "date",
  "dmesg", "journalctl", "ip", "ifconfig", "netstat", "ss", "lsof", "ping", "ping6", "dig",
  "nslookup", "traceroute", "mtr", "arp", "route", "curl", "wget", "telnet", "nc", "openssl",
  "jps", "jinfo", "nproc", "uname", "which", "type", "tree", "md5sum", "sha256sum", "wc",
  "sort", "uniq", "awk", "cut", "tr", "base64", "jq", "yq", "sqlite3",
]);

/** 剖析类程序：读但带明确分析意图，归 analyze（后端映射为 locate 阶段）。 */
const ANALYZE = new Set([
  "jstack", "jmap", "jstat", "jcmd", "jconsole", "arthas", "strace", "ltrace", "perf",
  "tcpdump", "wireshark", "pprof", "iostat", "sar", "mpstat", "pidstat", "dstat", "htop",
  "tcping", "hping3", "ethtool", "conntrack",
]);

/** 写文件类程序：内容变更，归 action。 */
const CONFIG_EDIT = new Set([
  "vi", "vim", "nvim", "nano", "emacs", "tee", "chmod", "chown", "ln", "mv",
  "cp", "mkdir", "touch", "truncate",
]);

const RESTART_PROGRAMS = new Set([
  "systemctl", "service", "nginx", "supervisorctl", "kill", "pkill", "killall", "shutdown",
  "reboot", "iptables", "ufw",
]);

/** kubectl 只读子命令。 */
const KUBECTL_READ = new Set([
  "get", "describe", "explain", "top", "version", "diff", "logs", "debug", "port-forward",
  "attach", "exec", "cluster-info", "api-resources", "config",
]);
/** kubectl 变更子命令 → 对应操作类型。 */
const KUBECTL_CHANGE: Record<string, { type: string; penalty: number }> = {
  apply: { type: "modify_config", penalty: 0 },
  patch: { type: "modify_config", penalty: 0 },
  edit: { type: "modify_config", penalty: 0 },
  replace: { type: "modify_config", penalty: 0 },
  annotate: { type: "modify_config", penalty: 0 },
  label: { type: "modify_config", penalty: 0 },
  taint: { type: "modify_config", penalty: 0 },
  set: { type: "modify_config", penalty: 0 },
  scale: { type: "scale_resource", penalty: 0 },
  rollout: { type: "restart_service", penalty: 0 },
  restart: { type: "restart_service", penalty: 0 },
  delete: { type: "restart_service", penalty: 1 },
  drain: { type: "restart_service", penalty: 1 },
  cordon: { type: "restart_service", penalty: 1 },
};

/** 会让服务中断的动词。 */
const STOP_WORDS = ["stop", "down", "uninstall", "rollback", "flushall", "flushdb"];

/** SQL / redis 数据变更动词。 */
const DATA_CHANGE = ["update", "insert", "delete", "alter", "drop", "truncate", "set", "del"];

/** 拆分管道与命令串联，逐段判定后取最强的一段（一段命令里既有查看又有重启应按重启记）。 */
const SEGMENT_SPLIT = /\s*(?:\|\||&&|[;\n|])\s*/;

const CATEGORY_SEVERITY: Record<OperationCategory, number> = {
  observe: 1,
  analyze: 2,
  ai_assist: 2,
  action: 3,
};

function firstWord(seg: string): { program: string; args: string[] } {
  const parts = seg.trim().split(/\s+/).filter(Boolean);
  while (parts.length > 0 && /^(?:[A-Za-z_][A-Za-z0-9_]*=|\w+=)/.test(parts[0])) {
    parts.shift();
  }
  if (parts.length > 0 && parts[0] === "sudo") parts.shift();
  const program = (parts[0] ?? "").toLowerCase().replace(/^\.\/|\.exe$/, "");
  return { program, args: parts.slice(1) };
}

/** 去掉包裹引号与 KEY=VAL 形式的参数，避免把 sed 表达式当成操作对象。 */
function positionalArgs(args: string[]): string[] {
  return args
    .filter((a) => !a.startsWith("-"))
    .map((a) => a.replace(/^['"]+|['"]+$/g, ""))
    .filter((a) => a && !/^[A-Za-z_][A-Za-z0-9_]*=/.test(a) && !/^[-'"]?[sSp]\//.test(a));
}

/** 取命令里的对象名（kubectl 的 resource/deploy 名、文件名、-n 之外的具名参数）。 */
function extractTarget(program: string, args: string[]): string {
  const positional = positionalArgs(args);
  if (program === "kubectl") {
    const sub = positional[0]?.toLowerCase() ?? "";
    const name = positional[1] ?? "";
    return [sub, name].filter(Boolean).join(" ").slice(0, 200);
  }
  if (program === "docker" || program === "helm" || program === "systemctl" || program === "service") {
    return positional.slice(0, 2).join(" ").slice(0, 200);
  }
  if (CONFIG_EDIT.has(program) || program === "rm") {
    const files = positional.filter((a) => a.includes("/") || a.includes("."));
    return (files[files.length - 1] ?? positional[0] ?? "").slice(0, 200);
  }
  const fileArg = positional.find((a) => a.includes("/") || a.includes("."));
  return (fileArg ?? positional[0] ?? "").slice(0, 200);
}

/** 输出重定向的目标路径。 */
function redirectPath(seg: string): string {
  const m = />>?\s*([^\s|;&]+)/.exec(seg);
  return m ? m[1].replace(/^['"]+|['"]+$/g, "") : "";
}

/** 落到 /tmp 的转储是分析产物，不算改配置。 */
function isScratchPath(p: string): boolean {
  return /^\/(tmp|var\/tmp)\//.test(p) || /^\.?\/?(tmp)\//.test(p);
}

function classifySegment(seg: string): CommandClass {
  const { program, args } = firstWord(seg);
  const target = extractTarget(program, args);
  const sub = (args[0] ?? "").toLowerCase();
  const joined = args.join(" ").toLowerCase();

  if (!program) {
    return { operationType: "exec_command", category: "observe", penalty: 0, target: "" };
  }

  const base = classifyProgram(program, sub, joined, args, target, seg);

  // 输出重定向：写向配置文件即变更；剖析类命令转储到 /tmp 仍按其本义归类。
  const redir = redirectPath(seg);
  if (redir && !isScratchPath(redir) && !ANALYZE.has(program)) {
    return {
      operationType: "modify_config",
      category: "action",
      penalty: base.penalty,
      target: redir,
    };
  }
  if (redir && base.category === "observe") {
    return { ...base, target: redir };
  }
  return base;
}

function classifyProgram(
  program: string,
  sub: string,
  joined: string,
  args: string[],
  target: string,
  seg: string,
): CommandClass {
  if (program === "kubectl") {
    const change = KUBECTL_CHANGE[sub];
    if (change) {
      return { operationType: change.type, category: "action", penalty: change.penalty, target };
    }
    if (sub === "logs") {
      return { operationType: "view_log", category: "observe", penalty: 0, target };
    }
    if (KUBECTL_READ.has(sub)) {
      return { operationType: "exec_command", category: "observe", penalty: 0, target };
    }
    return { operationType: "exec_command", category: "observe", penalty: 0, target };
  }

  if (program === "docker") {
    if (STOP_WORDS.includes(sub) || sub === "rm" || sub === "kill") {
      return {
        operationType: "restart_service",
        category: "action",
        penalty: sub === "rm" || sub === "kill" ? 1 : 0,
        target,
      };
    }
    if (["start", "restart"].includes(sub)) {
      return { operationType: "restart_service", category: "action", penalty: 0, target };
    }
    if (sub === "update" || sub === "commit") {
      return { operationType: "modify_config", category: "action", penalty: 0, target };
    }
    if (sub === "logs") {
      return { operationType: "view_log", category: "observe", penalty: 0, target };
    }
    if (sub === "inspect") {
      return { operationType: "view_config", category: "observe", penalty: 0, target };
    }
    return { operationType: "exec_command", category: "observe", penalty: 0, target };
  }

  if (program === "helm") {
    if (["upgrade", "install", "apply"].includes(sub)) {
      return { operationType: "modify_config", category: "action", penalty: 0, target };
    }
    if (STOP_WORDS.includes(sub)) {
      return { operationType: "restart_service", category: "action", penalty: 1, target };
    }
    return { operationType: "exec_command", category: "observe", penalty: 0, target };
  }

  if (RESTART_PROGRAMS.has(program)) {
    if (["status", "show", "is-active", "is-enabled", "list-units", "cat"].includes(sub)) {
      return { operationType: "exec_command", category: "observe", penalty: 0, target };
    }
    if (["kill", "pkill", "killall"].includes(program)) {
      return { operationType: "restart_service", category: "action", penalty: 1, target };
    }
    if (["start", "stop", "restart", "reload", "daemon-reload"].includes(sub)) {
      return {
        operationType: "restart_service",
        category: "action",
        penalty: sub === "stop" ? 1 : 0,
        target,
      };
    }
    if (sub === "-s" || joined.includes("reload")) {
      return { operationType: "restart_service", category: "action", penalty: 0, target };
    }
    if (program === "nginx" && (joined.includes("-t") || joined.includes("-v"))) {
      return { operationType: "view_config", category: "observe", penalty: 0, target };
    }
    if (["iptables", "ufw"].includes(program)) {
      return { operationType: "modify_config", category: "action", penalty: 0, target };
    }
    return { operationType: "exec_command", category: "observe", penalty: 0, target };
  }

  if (program === "redis-cli") {
    const verb = args.filter((a) => !a.startsWith("-"))[0]?.toLowerCase() ?? "";
    if (["flushall", "flushdb", "config", "set", "del", "expire"].includes(verb)) {
      return {
        operationType: "modify_config",
        category: "action",
        penalty: ["flushall", "flushdb", "del"].includes(verb) ? 1 : 0,
        target: verb,
      };
    }
    return { operationType: "exec_command", category: "observe", penalty: 0, target: verb };
  }

  if (["mysql", "psql", "mariadb"].includes(program)) {
    const sqlWords = joined;
    const hit = DATA_CHANGE.find((w) => new RegExp(`\\b${w}\\b`).test(sqlWords));
    if (hit) {
      const destructive = ["drop", "truncate", "delete", "alter"].includes(hit);
      return {
        operationType: destructive ? "restart_service" : "modify_config",
        category: "action",
        penalty: destructive ? 1 : 0,
        target: hit,
      };
    }
    if (/\bexplain\b/.test(sqlWords)) {
      return { operationType: "exec_command", category: "analyze", penalty: 0, target: program };
    }
    if (/\b(show|select|describe)\b/.test(sqlWords)) {
      return { operationType: "view_log", category: "observe", penalty: 0, target: program };
    }
    return { operationType: "exec_command", category: "observe", penalty: 0, target: program };
  }

  if (program === "rm") {
    return { operationType: "modify_config", category: "action", penalty: 1, target };
  }

  // sed 只在原地改文件（-i / --in-place）时算变更，否则是文本查看。
  if (program === "sed" && /(?:\s-i\b|--in-place)/.test(seg)) {
    return { operationType: "modify_config", category: "action", penalty: 0, target };
  }

  if (ANALYZE.has(program)) {
    return { operationType: "exec_command", category: "analyze", penalty: 0, target };
  }

  if (CONFIG_EDIT.has(program)) {
    return { operationType: "modify_config", category: "action", penalty: 0, target };
  }

  if (READ_ONLY.has(program)) {
    if (["tail", "less", "more", "cat"].includes(program) && /\.log($|\s)/.test(seg)) {
      return { operationType: "view_log", category: "observe", penalty: 0, target };
    }
    if (["cat", "less", "head", "tail"].includes(program)) {
      return { operationType: "view_config", category: "observe", penalty: 0, target };
    }
    return { operationType: "exec_command", category: "observe", penalty: 0, target };
  }

  // 未知程序保守记为 diag（不计入"观察充分"，避免误判为已验证）。
  return { operationType: "exec_command", category: "observe", penalty: 0, target };
}

/** 分类一条终端输入（可能含管道/串联，取最严重的一段）。 */
export function classifyCommand(line: string): CommandClass {
  const segments = line.split(SEGMENT_SPLIT).filter((s) => s.trim().length > 0);
  if (segments.length === 0) {
    return { operationType: "exec_command", category: "observe", penalty: 0, target: "" };
  }
  let worst = classifySegment(segments[0]);
  for (const seg of segments.slice(1)) {
    const cur = classifySegment(seg);
    if (CATEGORY_SEVERITY[cur.category] > CATEGORY_SEVERITY[worst.category]) worst = cur;
  }
  return worst;
}

/** 分页器/编辑器里的按键不是命令，别写进时间线。 */
const PAGER_KEYS = new Set([
  "q", "Q", "g", "G", "gg", "dG", "u", "i", "a", "o", "x", "d", "y", "p", "w", "wq", "wq!",
  "q!", "zz", "zZ", "H", "M", "L", "j", "k", "hh", "kk", "jj", "n", "N", "v", "V", "dd",
]);

/** 判断一行终端输入是否值得作为命令留痕。 */
export function isRecordableCommand(line: string): boolean {
  const t = line.trim();
  if (t.length < 2) return false;
  if (/^[/:?]/.test(t)) return false;
  if (PAGER_KEYS.has(t)) return false;
  return true;
}

/** 把一条终端命令转成上报载荷。 */
export function commandToOperation(line: string, pod: string): OperationInput {
  const c = classifyCommand(line);
  return {
    operation_type: c.operationType,
    operation_category: c.category,
    target: c.target ? `${pod}/${c.target}` : pod,
    detail: { command: line.slice(0, 500), source: "terminal" },
    penalty: c.penalty,
  };
}

/** 界面动作 → 上报载荷。 */
export function uiOperation(
  operationType: string,
  category: OperationCategory,
  target: string,
  source: string,
): OperationInput {
  return {
    operation_type: operationType,
    operation_category: category,
    target,
    detail: { source },
  };
}

/**
 * 终端输入行的本地重建：xterm 的 onData 会把一次按键/粘贴成片送来，
 * 需要自行处理回车、退格、Ctrl-C/U 与转义序列，才能得到完整命令。
 */
export function feedTerminalInput(
  buffer: string,
  data: string,
): { buffer: string; submitted: string[] } {
  const clean = data
    // eslint-disable-next-line no-control-regex -- Strip terminal ESC sequences before recording commands.
    .replace(/\x1b\[[0-9;?]*[A-Za-z]/g, "")
    // eslint-disable-next-line no-control-regex -- Strip terminal character-set selection sequences.
    .replace(/\x1b[()][A-Za-z0-9]/g, "")
    // eslint-disable-next-line no-control-regex -- Strip terminal keypad mode sequences.
    .replace(/\x1b[=>]/g, "")
    // eslint-disable-next-line no-control-regex -- Strip terminal application cursor sequences.
    .replace(/\x1bO[A-Za-z]/g, "");

  let buf = buffer;
  const submitted: string[] = [];
  for (const ch of clean) {
    const code = ch.charCodeAt(0);
    if (ch === "\r" || ch === "\n") {
      const line = buf.trim();
      if (line) submitted.push(line);
      buf = "";
    } else if (ch === "\x7f" || ch === "\b") {
      buf = buf.slice(0, -1);
    } else if (ch === "\x03" || ch === "\x15") {
      buf = "";
    } else if (code < 0x20) {
      continue;
    } else {
      buf += ch;
      if (buf.length > 512) buf = buf.slice(0, 512);
    }
  }
  return { buffer: buf, submitted };
}
