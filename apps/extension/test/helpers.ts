export const envelope = (kind: string, session: string, seq: number, body: object = {}, id?: string) => ({
  proto: "vsf-ext-v1",
  kind,
  id: id ?? `00000000-0000-4000-8000-${String(900_000_000_000 + seq).padStart(12, "0")}`,
  session,
  seq,
  body,
});
