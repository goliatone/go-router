# Prompt for Creating or Updating an Atomic Task Plan

## Objective

Your goal is to create or update a detailed, phased implementation plan for a software development task. The plan should be broken down into atomic, agent-executable tasks, following a strict format to ensure clarity, consistency, and efficient execution by an AI agent.

## Core Principles

- **Atomicity:** Each task must be small, self-contained, and have a single, testable outcome.
- **Clarity:** Instructions must be explicit, leaving no room for ambiguity. Specify exact file names, function signatures, and expected behaviors.
- **Phased Approach:** Group related tasks into logical phases (e.g., "Foundation," "Component Migration," "Testing") to ensure a structured workflow.
- **Agent-Friendliness:** The plan is for an AI agent. Optimize for minimal context switching and maximum clarity.
- **Technical Debt Awareness:** Use `TODO` comments to clearly mark any temporary implementations or shortcuts with a note on how to resolve them later.

## Task Examples

```
- [x] 2.2. Implement complete generation pipeline
  - Complete Generate, GenerateSchema, GenerateResolvers methods in pkg/gqlgen/generator.go
  - Wire together analyzer, schema generator, resolver generator, and template engine
  - Add error handling and progress reporting throughout pipeline
  - Implement context cancellation support
  - _Requirements: 1.1, 1.2, 2.1_


- [ ] 2.3. Implement SQL migration parser
  - Complete PatternMigrationParser implementation in internal/analyzer/migration_parser.go
  - Implement regex-based parsing methods (ParseFile, ParseDirectory, parseColumns, etc.)
  - Add support for parsing column definitions, constraints, and indexes
  - Write unit tests for migration parsing functionality
  - _Requirements: 1.1, 1.2, 1.7, 1.8_
```

---

## Instructions

### Scenario 1: Creating a New Task Plan from Scratch

1.  **Initial Analysis:**
    *   Perform a thorough analysis of the codebase to understand the current architecture, identify the key areas that need to be changed, and define a clear strategy for the implementation.
    *   If available, review any existing documentation (e.g., `ARCHITECTURE.md`, `PROTOTYPE_LEARNINGS.md`) to inform your strategy.

2.  **Define the Phases:**
    *   Break the project down into logical, high-level phases (e.g., data modeling, API implementation, UI components, testing, cleanup).
    *   Organize these phases sequentially to ensure a logical workflow.

3.  **Create Atomic Tasks:**
    *   Within each phase, create a list of atomic tasks.
    *   **Strictly adhere to the format defined in `TASK_TEMPLATE.md`**. This is not optional.
    *   For each task, ensure it meets the following criteria:
        *   **Scope:** Touches 1-3 files maximum.
        *   **Time:** Can be completed in 15-30 minutes.
        *   **Outcome:** Has one single, verifiable outcome.
        *   **Specificity:** Explicitly list all files to be created or modified.

4.  **Add Agent Directives:**
    *   At the beginning of each phase or major subsection, add an **"Agent Directive."**
    *   This directive should provide high-level guidance for the tasks in that section. It should not be generic. Tailor it to the specific challenges and goals of the phase (e.g., for a testing phase, "Focus on covering edge cases and mocking dependencies"; for a refactoring phase, "Ensure no breaking changes are introduced by running existing tests").

5.  **Final Review:**
    *   Review the complete plan for logical consistency, clarity, and adherence to the format.
    *   Ensure all dependencies between tasks are correctly documented using the `_Requirements:` tag.

### Scenario 2: Updating an Existing Task Plan

1.  **Analyze the Request:**
    *   Carefully read the user's request to understand what needs to be changed. This could be adding new tasks, reprioritizing existing ones, or modifying descriptions.

2.  **Review the Existing Plan:**
    *   Read the entire existing task plan to understand its structure, phases, and dependencies.

3.  **Implement the Changes:**
    *   **Adding New Tasks:** If adding new tasks, ensure they follow the same atomic format and are placed in the correct phase. Add a new phase if necessary.
    *   **Modifying Tasks:** If modifying a task, ensure the changes are clear and don't invalidate its dependencies or the overall plan.
    *   **Reprioritizing Tasks:** If moving tasks, be extremely careful to update the numbering of all subsequent tasks and, most importantly, update any `_Requirements:` tags that reference the moved or renumbered tasks.

4.  **Update Agent Directives:**
    *   If you've added or significantly changed a section, review the corresponding "Agent Directive" to ensure it's still relevant and provides the best possible guidance for the agent.

5.  **Final Review:**
    *   Read through the entire document one last time to ensure the changes are consistent with the rest of the plan and that all numbering and dependencies are correct.

---

## Final Output

The final output should be a single markdown file named `PROTOTYPE_REFACTORING_TASKS.md` (or a similar descriptive name) that is ready for an AI agent to begin executing, starting with the first uncompleted task.
