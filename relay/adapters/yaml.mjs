// Homing relay · 适配器清单用的最小 YAML 子集解析（递归下降）
// 支持：注释、嵌套 map、block 数组（- 项）、flow 数组 [a, b]、引号、~ null、true/false、数字。
// 不支持：锚点、多文档、块折叠（| >）。解析失败抛错并带行号。
// 目的：清单是人手写的，够用即可，但**不能静默**。

function stripComment(s) {
  let q = null;
  for (let i = 0; i < s.length; i++) {
    const ch = s[i];
    if (q) {
      if (ch === q && s[i - 1] !== "\\") q = null;
    } else if (ch === '"' || ch === "'") {
      q = ch;
    } else if (ch === "#" && (i === 0 || /\s/.test(s[i - 1]))) {
      return s.slice(0, i);
    }
  }
  return s;
}

function scalar(raw) {
  const v = String(raw ?? "").trim();
  if (v === "" || v === "~" || v === "null") return null;
  if (v === "true") return true;
  if (v === "false") return false;
  if (/^-?\d+$/.test(v)) return Number(v);
  if (/^-?\d+\.\d+$/.test(v)) return Number(v);
  if (v.startsWith("[") && v.endsWith("]")) {
    const inner = v.slice(1, -1).trim();
    if (!inner) return [];
    return inner.split(",").map((x) => scalar(x));
  }
  if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
    return v.slice(1, -1);
  }
  return v;
}

function isListItem(line) {
  return /^-\s/.test(line) || line === "-";
}

// 返回 [value, nextIndex]
function parseBlock(lines, start, indent) {
  const first = lines[start];
  if (!first) return [null, start];

  if (isListItem(first.text)) {
    const arr = [];
    let i = start;
    while (i < lines.length && lines[i].indent === indent && isListItem(lines[i].text)) {
      const itemText = lines[i].text.replace(/^-\s?/, "");
      if (itemText === "") {
        // 数组项里是嵌套块
        const [child, next] = parseBlock(lines, i + 1, lines[i + 1]?.indent ?? indent + 2);
        arr.push(child);
        i = next;
      } else if (/^[^:\s][^:]*:(\s|$)/.test(itemText)) {
        // "- key: value" 形式的 map 项：把这一行当 map 的第一行，后续同缩进的键并入
        const obj = {};
        const m = /^([^:]+):(.*)$/.exec(itemText);
        obj[m[1].trim()] = m[2].trim() === "" ? null : scalar(m[2]);
        i += 1;
        const childIndent = indent + 2;
        while (i < lines.length && lines[i].indent >= childIndent && !isListItem(lines[i].text)) {
          const mm = /^([^:]+):(.*)$/.exec(lines[i].text);
          if (!mm) break;
          const key = mm[1].trim();
          if (mm[2].trim() === "") {
            const [child, next] = parseBlock(lines, i + 1, lines[i + 1]?.indent ?? childIndent + 2);
            obj[key] = child;
            i = next;
          } else {
            obj[key] = scalar(mm[2]);
            i += 1;
          }
        }
        arr.push(obj);
      } else {
        arr.push(scalar(itemText));
        i += 1;
      }
    }
    return [arr, i];
  }

  const obj = {};
  let i = start;
  while (i < lines.length && lines[i].indent === indent && !isListItem(lines[i].text)) {
    const m = /^([^:]+):(.*)$/.exec(lines[i].text);
    if (!m) throw new Error(`第 ${lines[i].no} 行无法解析：${lines[i].text}`);
    const key = m[1].trim();
    const rest = m[2];
    if (rest.trim() === "") {
      const nextLine = lines[i + 1];
      if (nextLine && nextLine.indent > indent) {
        const [child, next] = parseBlock(lines, i + 1, nextLine.indent);
        obj[key] = child;
        i = next;
      } else {
        obj[key] = null;
        i += 1;
      }
    } else {
      obj[key] = scalar(rest);
      i += 1;
    }
  }
  return [obj, i];
}

export function parseYaml(text) {
  const raw = String(text ?? "").split(/\r?\n/);
  const lines = [];
  raw.forEach((line, idx) => {
    const noComment = stripComment(line);
    if (!noComment.trim()) return;
    const indent = noComment.length - noComment.replace(/^ +/, "").length;
    lines.push({ indent, text: noComment.trim(), no: idx + 1 });
  });
  if (!lines.length) return {};
  const [value] = parseBlock(lines, 0, lines[0].indent);
  return value;
}

export function expandHome(p, home) {
  if (typeof p !== "string" || !p) return p;
  if (p === "~") return home;
  if (p.startsWith("~/")) return `${home}${p.slice(1)}`;
  return p;
}

export function parseYamlFile(fs, filePath) {
  return parseYaml(fs.readFileSync(filePath, "utf8"));
}
