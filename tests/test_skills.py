from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKILLS = ROOT / "skills"
WORKFLOW = SKILLS / "worklease-workflow"
LINK = re.compile(r"\[[^]]*]\(([^)]+)\)")


class SkillBundleTests(unittest.TestCase):
    def test_worklease_is_one_self_contained_skill(self) -> None:
        self.assertTrue((WORKFLOW / "SKILL.md").is_file())
        self.assertFalse((SKILLS / "worklease-source-workflow/SKILL.md").exists())
        self.assertTrue((WORKFLOW / "LICENSE.txt").is_file())
        self.assertTrue((WORKFLOW / "references/contract.md").is_file())
        self.assertTrue((WORKFLOW / "references/source-workflow.md").is_file())
        self.assertTrue((WORKFLOW / "references/source-provider-contract.md").is_file())

    def test_skill_frontmatter_is_portable(self) -> None:
        text = (WORKFLOW / "SKILL.md").read_text(encoding="utf-8")
        _, frontmatter, _ = text.split("---", 2)
        fields = {
            line.split(":", 1)[0] for line in frontmatter.splitlines() if line.strip()
        }
        self.assertEqual({"name", "description"}, fields)
        self.assertIn("name: worklease-workflow", frontmatter)

    def test_skill_links_exist_and_stay_inside_skill_root(self) -> None:
        root = WORKFLOW.resolve()
        for document in WORKFLOW.rglob("*.md"):
            for target in LINK.findall(document.read_text(encoding="utf-8")):
                if "://" in target or target.startswith("#"):
                    continue
                resolved = (document.parent / target.split("#", 1)[0]).resolve()
                self.assertTrue(resolved.is_relative_to(root), (document, target))
                self.assertTrue(resolved.is_file(), (document, target))

    def test_agent_installation_guidance_names_complete_bundle(self) -> None:
        guidance = (SKILLS / "AGENTS.md").read_text(encoding="utf-8")
        self.assertIn("complete", guidance)
        self.assertIn("worklease-workflow/", guidance)
        self.assertIn("do not install", guidance)
        self.assertIn("do not assume", guidance.lower())


if __name__ == "__main__":
    unittest.main()
