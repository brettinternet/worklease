# Installing the Worklease skill

When a user asks to install the Worklease agent skill, install the complete
`worklease-workflow/` directory. It contains the provider-neutral workflow,
source-provider mappings, examples, and every referenced file; do not install
individual files or reconstruct the skill from this `AGENTS.md`.

The canonical source is `https://github.com/brettinternet/worklease`, under
`skills/worklease-workflow/`.

1. Confirm where the user's agent discovers Agent Skills. Prefer the agent's
   native skill installer when it can install a repository subdirectory.
   Otherwise copy `skills/worklease-workflow/` recursively into the user's
   chosen user-, project-, or workspace-level skills directory.
2. Use a tagged Worklease release when the CLI is version-pinned. Preserve the
   directory name `worklease-workflow`, and do not overwrite an existing skill
   without the user's approval.
3. Confirm the installed directory contains `SKILL.md`,
   `LICENSE.txt`, `references/contract.md`, and the source-provider references.
   Validate it with the agent's Agent Skills validator when one is available.
4. Confirm `worklease --version` is available to the agent. Installing the
   skill does not install the CLI; direct the user to the repository README for
   CLI installation.

Do not assume a Codex-, Claude-, or other product-specific skills path. If the
agent has no native installer or documented discovery directory, ask the user
to select the destination instead of inventing one.
