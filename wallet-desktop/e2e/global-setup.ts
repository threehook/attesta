// What the end-to-end tests need around the Go issuer: a Postgres and a mail catcher in containers, and the issuer built once.
// Needs Docker and Go; the containers and the binary are removed when the tests end.
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const docker = (...args: string[]) => execFileSync("docker", args, { encoding: "utf8" }).trim();
const hostPort = (container: string, port: string) => docker("port", container, port).split("\n")[0].split(":").pop()!;
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

async function until(what: string, ready: () => Promise<boolean> | boolean): Promise<void> {
  for (let i = 0; i < 100; i++) {
    try {
      if (await ready()) return;
    } catch {
      // not up yet
    }
    await sleep(300);
  }
  throw new Error(`${what} did not come up`);
}

export default async function globalSetup() {
  const postgres = docker("run", "-d", "--rm", "-e", "POSTGRES_PASSWORD=e2e", "-p", "127.0.0.1::5432", "postgres:17-alpine");
  const mail = docker("run", "-d", "--rm", "-p", "127.0.0.1::1025", "-p", "127.0.0.1::8025", "axllent/mailpit");
  const dir = mkdtempSync(join(tmpdir(), "attesta-e2e-issuer-"));
  const teardown = async () => {
    for (const container of [postgres, mail]) {
      try {
        docker("rm", "-f", container);
      } catch {
        // already gone
      }
    }
    rmSync(dir, { recursive: true, force: true });
  };

  try {
    execFileSync("go", ["build", "-o", join(dir, "issuer"), "./cmd/issuer"], { cwd: join(import.meta.dirname, "..", "..", "issuer", "api"), stdio: "inherit" });
    // The image starts a temporary server before the real one, so the database is ready at the second "ready" message.
    await until("postgres", () => {
      const logs = spawnSync("docker", ["logs", postgres], { encoding: "utf8" });
      return (logs.stdout + logs.stderr).split("ready to accept connections").length > 2;
    });
    const api = `http://127.0.0.1:${hostPort(mail, "8025/tcp")}`;
    await until("the mail catcher", async () => (await fetch(`${api}/livez`)).ok);

    process.env.ATTESTA_E2E_ISSUER_BIN = join(dir, "issuer");
    process.env.ATTESTA_E2E_POSTGRES = postgres;
    process.env.ATTESTA_E2E_POSTGRES_PORT = hostPort(postgres, "5432/tcp");
    process.env.ATTESTA_E2E_SMTP_PORT = hostPort(mail, "1025/tcp");
    process.env.ATTESTA_E2E_MAIL_API = api;
  } catch (error) {
    await teardown();
    throw error;
  }
  return teardown;
}
