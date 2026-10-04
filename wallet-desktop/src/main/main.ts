import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { app, BrowserWindow, dialog, ipcMain, safeStorage } from "electron";
import { createWalletAgent, type WalletAgent } from "@attesta/wallet";
import { channels, type Result } from "../shared/api.js";
import { devSwitches } from "./dev-switches.js";
import { loadOrCreateStoreKey } from "./keystore.js";
import { WalletService } from "./service.js";

const here = fileURLToPath(new URL(".", import.meta.url));
const LINK_SCHEMES = ["openid-credential-offer", "openid4vp"];

// Development and tests can change where the wallet keeps its data, which key protects it, and allow plain http; an installed app cannot.
const dev = devSwitches(process.env, app.isPackaged);
if (dev.dataDir) {
  app.setPath("userData", dev.dataDir);
}

if (!app.requestSingleInstanceLock()) {
  app.quit();
}

let window: BrowserWindow | undefined;
let agent: WalletAgent | undefined;
const queuedLinks: string[] = [];

const isWalletLink = (arg: string) => LINK_SCHEMES.some((scheme) => arg.startsWith(`${scheme}:`));

function deliverLink(link: string) {
  if (window && !window.webContents.isLoading()) {
    window.webContents.send(channels.link, link);
    if (window.isMinimized()) window.restore();
    window.focus();
  } else {
    queuedLinks.push(link);
  }
}

// Registering as the handler for these schemes changes the user's system, so a development run only does it when asked.
if (app.isPackaged || process.env.ATTESTA_REGISTER_PROTOCOLS === "1") {
  for (const scheme of LINK_SCHEMES) app.setAsDefaultProtocolClient(scheme);
}

app.on("open-url", (event, url) => {
  event.preventDefault();
  deliverLink(url);
});
app.on("second-instance", (_event, argv) => {
  for (const arg of argv.filter(isWalletLink)) deliverLink(arg);
});

// The renderer shows our own UI only: no navigation away from it, no new windows, no permissions.
app.on("web-contents-created", (_event, contents) => {
  contents.on("will-navigate", (event) => event.preventDefault());
  contents.setWindowOpenHandler(() => ({ action: "deny" }));
  contents.session.setPermissionRequestHandler((_wc, _permission, callback) => callback(false));
});

async function handled<T>(work: () => Promise<T> | T): Promise<Result<T>> {
  try {
    return { ok: true, value: await work() };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : String(error) };
  }
}

function createWindow() {
  window = new BrowserWindow({
    width: 900,
    height: 680,
    show: false,
    webPreferences: { preload: join(here, "../preload/preload.cjs"), contextIsolation: true, sandbox: true, nodeIntegration: false },
  });
  window.once("ready-to-show", () => window?.show());
  window.webContents.on("did-finish-load", () => {
    for (const link of queuedLinks.splice(0)) window?.webContents.send(channels.link, link);
  });
  void window.loadFile(join(here, "../renderer/index.html"));
}

async function start() {
  const userData = app.getPath("userData");
  const storeKey =
    dev.storeKey ??
    loadOrCreateStoreKey(join(userData, "store-key"), {
      isAvailable: () => safeStorage.isEncryptionAvailable(),
      encrypt: (plain) => safeStorage.encryptString(plain),
      decrypt: (encrypted) => safeStorage.decryptString(encrypted),
    });
  agent = await createWalletAgent({
    storeId: "wallet",
    storeKey,
    path: join(userData, "wallet"),
    allowInsecureHttp: dev.allowInsecureHttp,
  });
  const service = new WalletService(agent);

  ipcMain.handle(channels.list, () => handled(() => service.list()));
  ipcMain.handle(channels.prepare, (_event, link: string) => handled(() => service.prepare(link)));
  ipcMain.handle(channels.approve, (_event, id: string) => handled(() => service.approve(id)));
  ipcMain.handle(channels.decline, (_event, id: string) => handled(() => service.decline(id) ?? null));

  queuedLinks.push(...process.argv.filter(isWalletLink));
  createWindow();
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
}

// A wallet that cannot open its store has nothing to show, so say why instead of failing silently.
app
  .whenReady()
  .then(start)
  .catch((error) => {
    const message = error instanceof Error ? (error.cause instanceof Error ? `${error.message}\n\n${error.cause.message}` : error.message) : String(error);
    console.error("attesta wallet could not start:", error);
    dialog.showErrorBox("attesta wallet could not start", message);
    app.quit();
  });

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});

app.on("before-quit", () => {
  void agent?.shutdown();
});
