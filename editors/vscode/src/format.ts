// The pure part of Format Document: how `apic fmt -` output becomes an
// edit. Kept free of the vscode module so it is unit-tested under node.

/** The one edit that turns `text` into `formatted`, or undefined when they are the same. */
export function fullDocumentEdit(text: string, formatted: string): { start: number; end: number; newText: string } | undefined {
  if (formatted === text) {
    return undefined;
  }
  return { start: 0, end: text.length, newText: formatted };
}
