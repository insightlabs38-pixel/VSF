import { readFileSync } from "node:fs";

const LINE_COMMENT: Record<string, RegExp> = {
  rs: /^\s*\/\//,
  ts: /^\s*\/\//,
  mjs: /^\s*\/\//,
  go: /^\s*\/\//,
  sh: /^\s*#(?!!)/,
};

export interface CommentViolation {
  file: string;
  line: number;
  message: string;
}

/** A comment may occupy one physical line at most. */
export function lintComments(file: string, text: string): CommentViolation[] {
  const ext = file.split(".").pop() ?? "";
  const re = LINE_COMMENT[ext];
  if (!re) return [];
  const out: CommentViolation[] = [];
  const lines = text.split("\n");
  let inBlock = false;
  let blockStart = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? "";
    if (ext !== "sh") {
      if (inBlock) {
        if (line.includes("*/")) {
          inBlock = false;
          out.push({ file, line: blockStart + 1, message: "block comment spans multiple lines" });
        }
        continue;
      }
      const open = line.indexOf("/*");
      if (
        open >= 0 &&
        !line.slice(0, open).includes('"') &&
        !line.slice(0, open).includes("'") &&
        !line.slice(0, open).includes("`")
      ) {
        if (!line.includes("*/", open + 2)) {
          inBlock = true;
          blockStart = i;
          continue;
        }
      }
    }
    if (re.test(line) && re.test(lines[i + 1] ?? "")) {
      out.push({ file, line: i + 1, message: "comment spans multiple lines" });
      while (re.test(lines[i + 1] ?? "")) i++;
    }
  }
  return out;
}

export function lintCommentFiles(files: string[]): CommentViolation[] {
  return files.flatMap((f) => lintComments(f, readFileSync(f, "utf8")));
}
