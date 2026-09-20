import { defineConfig } from "@vscode/test-cli";

// Runs the compiled tests inside a real VS Code, opened on the fixture
// project, so activation, commands and settings are exercised for real.
export default defineConfig({
  files: "out/test/*.test.js",
  workspaceFolder: "src/test/fixture",
  mocha: { ui: "tdd", timeout: 20000 },
});
