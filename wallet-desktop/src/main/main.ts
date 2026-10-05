import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { app, BrowserWindow, dialog, ipcMain, Notification, safeStorage } from "electron";
import { createWalletAgent, type WalletAgent } from "@attesta/wallet";
import { channels, type Pending, type Result, type Shared, type SignInChoice } from "../shared/api.js";
import { devSwitches } from "./dev-switches.js";
import { IDENTITY_PORT, startIdentityServer } from "./identity-server.js";
import { loadOrCreateStoreKey } from "./keystore.js";
import { WalletService } from "./service.js";
import { decision, nothingToShare, promptFor } from "./prompt.js";
import { applicationOf, WalletSettings } from "./settings.js";

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
let service: WalletService | undefined;
// Links that arrived before the wallet had started, and what the window shows once it has loaded.
const queuedLinks: string[] = [];
const queuedResults: Result<Pending>[] = [];

const isWalletLink = (arg: string) => LINK_SCHEMES.some((scheme) => arg.startsWith(`${scheme}:`));

function deliverLink(link: string) {
  if (service) void handleLink(service, link);
  else queuedLinks.push(link);
}

// Reads a link the system handed to the app. A presentation request never opens the window: it is answered when the user chose to share with the
// application, and otherwise asked about in a popup. Only a credential offer is shown in the window.
async function handleLink(wallet: WalletService, link: string) {
  const result = await handled(() => wallet.prepare(link));
  const presentation = link.trim().startsWith("openid4vp:");
  if (!result.ok) {
    if (presentation) dialog.showErrorBox("Could not read the request", result.error);
    else show(result);
  } else if (result.value.kind === "shared") {
    announce(result.value);
  } else if (result.value.kind === "presentation") {
    await askToShare(wallet, result.value);
  } else {
    show({ ok: true, value: result.value });
  }
}

async function askToShare(wallet: WalletService, request: Extract<Pending, { kind: "presentation" }>) {
  app.focus({ steal: true });
  if (!request.satisfiable) {
    wallet.decline(request.id);
    await dialog.showMessageBox({ type: "info", ...nothingToShare(request), buttons: ["OK"] });
    return;
  }
  const prompt = promptFor(request);
  const { response, checkboxChecked } = await dialog.showMessageBox({
    type: "question",
    message: prompt.message,
    detail: prompt.detail,
    buttons: prompt.buttons,
    defaultId: 0,
    cancelId: prompt.buttons.length - 1,
    checkboxLabel: prompt.checkboxLabel,
  });
  const answer = decision(prompt, response, checkboxChecked);
  if (answer === "decline") {
    wallet.decline(request.id);
    return;
  }
  const done = await handled(() => wallet.approve(request.id, answer === "always"));
  if (answer === "always") window?.webContents.send(channels.settingsChanged);
  if (!done.ok) dialog.showErrorBox("Could not share", done.error);
  else if (done.value.kind === "presentation") announce({ kind: "shared", verifier: request.verifier, claims: request.requested.flatMap((r) => r.claims), status: done.value.status });
}

function show(result: Result<Pending>) {
  if (window && !window.webContents.isLoading()) {
    window.webContents.send(channels.link, result);
    if (window.isMinimized()) window.restore();
    window.focus();
  } else {
    queuedResults.push(result);
    // On macOS the app outlives its window; what then needs showing gets a new one, which sends it when it has loaded.
    if (!window) createWindow();
  }
}

function announce(shared: Shared) {
  if (!Notification.isSupported()) return;
  const who = applicationOf(shared.verifier) ?? shared.verifier;
  const sent = shared.status < 400;
  new Notification({ title: sent ? "Shared" : "Not shared", body: sent ? `${shared.claims.join(", ")} with ${who}` : `${who} did not accept the answer` }).show();
}

// Registering as the handler for these schemes changes the user's system, so only an installed app does it.
if (app.isPackaged) {
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
  window.once("closed", () => {
    window = undefined;
  });
  window.webContents.on("did-finish-load", () => {
    for (const result of queuedResults.splice(0)) window?.webContents.send(channels.link, result);
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
  const wallet = new WalletService(agent, new WalletSettings(join(userData, "settings.json")));
  service = wallet;

  ipcMain.handle(channels.list, () => handled(() => wallet.list()));
  ipcMain.handle(channels.prepare, (_event, link: string) => handled(() => wallet.prepare(link)));
  ipcMain.handle(channels.approve, (_event, id: string, remember?: boolean) => handled(() => wallet.approve(id, remember === true)));
  ipcMain.handle(channels.decline, (_event, id: string) => handled(() => wallet.decline(id) ?? null));
  ipcMain.handle(channels.settings, () => handled(() => wallet.settingsView()));
  ipcMain.handle(channels.forgetApplication, (_event, application: string) => handled(() => wallet.forgetApplication(String(application))));
  ipcMain.handle(channels.forgetSignIn, (_event, choice: SignInChoice) =>
    handled(() => wallet.forgetSignIn({ application: String(choice.application), email: String(choice.email), issuer: String(choice.issuer) })),
  );
  // Pages ask the wallet who the user is; when the port is taken the wallet still works, a page just cannot offer the choice.
  startIdentityServer(wallet, dev.identityPort ?? IDENTITY_PORT, (message) => console.error(message)).catch((error) =>
    console.error("the identity port could not be opened:", error),
  );

  const links = [...queuedLinks.splice(0), ...process.argv.filter(isWalletLink)];
  await Promise.all(links.map((link) => handleLink(wallet, link)));
  // A start that only had links to answer without a window needs none; started on its own, the wallet shows itself.
  if (!window && links.length === 0) createWindow();
  else if (!window && process.platform !== "darwin") app.quit();
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
