"""On-demand skill loading (s07).

A skill is a `SKILL.md` file with YAML-ish frontmatter (`name`, `description`)
plus a markdown body. At startup we index only the *descriptions* -- cheap, a
line each -- and inject a full body into context only when the model asks via
the `load_skill` tool. Knowledge on demand, not upfront.

The loader is read-only, so a single instance is safely shared by every
concurrent session.
"""

from __future__ import annotations

import re
from pathlib import Path

_FRONTMATTER = re.compile(r"^---\n(.*?)\n---\n(.*)", re.DOTALL)


class SkillLoader:
    def __init__(self, skills_dir: Path) -> None:
        self.skills: dict[str, dict] = {}
        if skills_dir.exists():
            for f in sorted(skills_dir.rglob("SKILL.md")):
                text = f.read_text()
                meta: dict[str, str] = {}
                body = text
                m = _FRONTMATTER.match(text)
                if m:
                    for line in m.group(1).strip().splitlines():
                        if ":" in line:
                            k, v = line.split(":", 1)
                            meta[k.strip()] = v.strip()
                    body = m.group(2).strip()
                name = meta.get("name", f.parent.name)
                self.skills[name] = {"meta": meta, "body": body}

    def descriptions(self) -> str:
        if not self.skills:
            return "(no skills available)"
        return "\n".join(
            f"  - {n}: {s['meta'].get('description', '-')}" for n, s in self.skills.items()
        )

    def load(self, name: str) -> str:
        s = self.skills.get(name)
        if not s:
            available = ", ".join(self.skills) or "(none)"
            return f"Error: Unknown skill '{name}'. Available: {available}"
        return f'<skill name="{name}">\n{s["body"]}\n</skill>'
