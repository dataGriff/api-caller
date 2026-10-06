import { defineConfig } from "@vscode/test-cli";

// Runs the compiled tests inside a real VS Code, opened on the fixture
// project, so activation, commands and settings are exercised for real.
// The suite runs twice: with the extension's own diagnostics, completion
// and hover, and with apic.languageServer.enable taking them from
// `apic lsp` (the workspace file sets it), which must pass the same tests.
const mocha = { ui: "tdd", timeout: 20000 };
export default defineConfig([
  { label: "extension", files: "out/test/*.test.js", workspaceFolder: "src/test/fixture", mocha },
  { label: "language server", files: "out/test/*.test.js", workspaceFolder: "src/test/fixture-lsp.code-workspace", mocha },
]);
