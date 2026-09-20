import * as assert from "node:assert";
import { fullDocumentEdit } from "../../format";

suite("formatter", () => {
  test("one edit over the whole document, none when nothing changes", () => {
    assert.strictEqual(fullDocumentEdit("GET http://x\n", "GET http://x\n"), undefined);
    assert.deepStrictEqual(fullDocumentEdit("GET  http://x  \n", "GET http://x\n"), { start: 0, end: 16, newText: "GET http://x\n" });
  });
});
