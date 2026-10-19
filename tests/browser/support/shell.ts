import type { Page } from "@playwright/test";

interface AxNode {
  nodeId: string;
  ignored?: boolean;
  role?: { value: string };
  name?: { value: string };
  backendDOMNodeId?: number;
  properties?: { name: string; value: { value: unknown } }[];
}

export interface Ax {
  role: string;
  name: string;
  disabled: boolean;
  backendId: number | undefined;
}

/** What assistive technology is offered; the browser's tree reaches into closed shadow roots. */
export async function axNodes(page: Page): Promise<Ax[]> {
  const session = await page.context().newCDPSession(page);
  const { nodes } = (await session.send("Accessibility.getFullAXTree")) as { nodes: AxNode[] };
  await session.detach();
  return nodes
    .filter((n) => !n.ignored)
    .map((n) => ({
      role: n.role?.value ?? "",
      name: String(n.name?.value ?? ""),
      disabled: n.properties?.some((p) => p.name === "disabled" && p.value.value === true) ?? false,
      backendId: n.backendDOMNodeId,
    }));
}

/** A real mouse press at the control's centre, so the renderer sees a trusted activation. */
export async function clickInShell(page: Page, role: string, name: RegExp | string): Promise<void> {
  const match = (n: string): boolean => (typeof name === "string" ? n.includes(name) : name.test(n));
  const target = (await axNodes(page)).find((n) => n.role === role && match(n.name));
  if (target?.backendId === undefined) throw new Error(`no ${role} named ${String(name)} in the shell`);
  const session = await page.context().newCDPSession(page);
  await session.send("DOM.scrollIntoViewIfNeeded", { backendNodeId: target.backendId });
  const { model } = (await session.send("DOM.getBoxModel", { backendNodeId: target.backendId })) as {
    model: { content: number[] };
  };
  const [x1, y1, , , x3, y3] = model.content as [number, number, number, number, number, number];
  const x = (x1 + x3) / 2;
  const y = (y1 + y3) / 2;
  await session.send("Input.dispatchMouseEvent", { type: "mouseMoved", x, y });
  await session.send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "left", clickCount: 1 });
  await session.send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "left", clickCount: 1 });
  await session.detach();
}
