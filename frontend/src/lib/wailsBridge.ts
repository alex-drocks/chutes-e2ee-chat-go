type WailsApp = {
  Chat: (payload: unknown) => Promise<unknown>;
  Abort: (payload: unknown) => Promise<unknown>;
  Models: () => Promise<unknown>;
  ModelStats: () => Promise<unknown>;
  WebSearch: (payload: unknown) => Promise<unknown>;
  ClipboardImage: () => Promise<unknown>;
  SaveApiKey: (payload: unknown) => Promise<unknown>;
  GetApiKeyStatus: (payload: unknown) => Promise<unknown>;
  DeleteApiKey: (payload: unknown) => Promise<unknown>;
};

type WailsRuntime = {
  EventsOn: (eventName: string, callback: (payload: unknown) => void) => (() => void) | void;
  EventsOff: (eventName: string, ...additionalEventNames: string[]) => void;
};

type WailsWindow = Window &
  typeof globalThis & {
    go?: {
      main?: {
        App?: WailsApp;
      };
    };
    runtime?: WailsRuntime;
  };

export function installWailsBridge() {
  if (typeof window === 'undefined') return;

  const target = window as WailsWindow;
  if (target.chutes) return;

  const call = <T,>(method: keyof WailsApp, payload?: unknown): Promise<T> => {
    const app = target.go?.main?.App;
    const fn = app?.[method] as ((payload?: unknown) => Promise<T>) | undefined;
    if (typeof fn !== 'function') {
      return Promise.resolve({
        ok: false,
        error: `Wails binding App.${String(method)} is not available yet.`,
      } as T);
    }
    return payload === undefined ? fn() : fn(payload);
  };

  const on = <T,>(eventName: string, callback: (payload: T) => void) => {
    const runtime = target.runtime;
    if (!runtime?.EventsOn) return () => {};

    const dispose = runtime.EventsOn(eventName, (payload) => callback(payload as T));
    if (typeof dispose === 'function') return dispose;
    return () => runtime.EventsOff?.(eventName);
  };

  target.chutes = {
    chat: (requestId, params) => call('Chat', { requestId, params }),
    abort: (requestId) => call('Abort', { requestId }),
    models: () => call('Models'),
    modelStats: () => call('ModelStats'),
    webSearch: (query, deepSearch = false) => call('WebSearch', { query, deepSearch }),
    clipboardImage: () => call('ClipboardImage'),
    onStreamChunk: (callback) => on('chutes:chunk', callback),
    onStreamError: (callback) => on('chutes:error', callback),
    saveApiKey: (provider, apiKey) => call('SaveApiKey', { provider, apiKey }),
    getApiKeyStatus: (provider) => call('GetApiKeyStatus', { provider }),
    deleteApiKey: (provider) => call('DeleteApiKey', { provider }),
  };
}
