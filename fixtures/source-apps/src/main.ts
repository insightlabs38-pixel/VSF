import { startServer } from "./server.ts";

const { url } = await startServer(Number(process.env.PORT ?? 4173));
console.log(`fixture apps listening on ${url}`);
