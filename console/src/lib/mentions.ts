export interface Mention {
  agent: string;
  preset: string;
  text: string;
  start: number;
  end: number;
  dispatchable: boolean;
}

export interface MentionPlan {
  message: string;
  mentions: Mention[];
}

interface Hit {
  agent: string;
  preset: string;
  start: number;
  end: number;
}

const mentionPattern = /@([a-z0-9][a-z0-9._-]{0,63})(?::([a-z-]{1,24}))?/gi;

function insideMentionToken(line: string, index: number): boolean {
  if (index <= 0) return false;
  return /[a-z0-9._%+-]/i.test(line[index - 1]);
}

function insideInlineCode(line: string, index: number): boolean {
  let ticks = 0;
  for (let position = 0; position < Math.min(index, line.length); position++) {
    if (line[position] === "`") ticks++;
  }
  return ticks % 2 === 1;
}

export function parseMentions(
  input: string,
  dispatchableAgents: Iterable<string>,
): MentionPlan {
  const trimmed = input.trim();
  if (!trimmed) return { message: "", mentions: [] };
  const known = new Set(Array.from(dispatchableAgents, (agent) => agent.toLowerCase()));
  const hits: Hit[] = [];
  let fence = "";
  let lineStart = 0;
  while (lineStart <= input.length) {
    const newline = input.indexOf("\n", lineStart);
    const lineEnd = newline < 0 ? input.length : newline;
    const line = input.slice(lineStart, lineEnd).replace(/\r$/, "");
    const trimLine = line.trimStart();
    const marker = trimLine.startsWith("```") ? "```" : trimLine.startsWith("~~~") ? "~~~" : "";
    if (marker) {
      if (!fence) fence = marker;
      else if (fence === marker) fence = "";
    } else if (!fence) {
      mentionPattern.lastIndex = 0;
      for (const match of line.matchAll(mentionPattern)) {
        const start = match.index ?? 0;
        if (insideMentionToken(line, start) || insideInlineCode(line, start)) continue;
        hits.push({
          agent: match[1].toLowerCase(),
          preset: (match[2] || "ask").toLowerCase(),
          start: lineStart + start,
          end: lineStart + start + match[0].length,
        });
      }
    }
    if (newline < 0) break;
    lineStart = lineEnd + 1;
  }
  if (!hits.length) return { message: trimmed, mentions: [] };
  return {
    message: input.slice(0, hits[0].start).trim(),
    mentions: hits.map((hit, index) => ({
      agent: hit.agent,
      preset: hit.preset,
      text: input.slice(hit.end, hits[index + 1]?.start ?? input.length).trim(),
      start: hit.start,
      end: hit.end,
      dispatchable: known.has(hit.agent),
    })),
  };
}
