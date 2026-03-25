import { existsSync, mkdirSync, rmSync, watch } from "fs";
import { relative, resolve } from "path";
import postcss from "postcss";
import tailwindcss from "@tailwindcss/postcss";

const API_TARGET = process.env.API_TARGET ?? "http://127.0.0.1:8181";
const DEV_PORT = parseInt(process.env.DEV_PORT ?? "5173", 10);
const devDistDir = resolve("./.dev-dist");
const srcDir = resolve("./src");
const indexHtmlPath = resolve("./index.html");

let latestBuildError = "";
let rebuildTimer: ReturnType<typeof setTimeout> | null = null;
let buildQueue = Promise.resolve();

const tailwindPlugin: import("bun").BunPlugin = {
  name: "tailwind-css",
  setup(build) {
    build.onLoad({ filter: /\.css$/ }, async (args) => {
      const source = await Bun.file(args.path).text();
      const result = await postcss([tailwindcss()]).process(source, {
        from: args.path,
      });

      return {
        contents: result.css,
        loader: "css",
      };
    });
  },
};

function relPath(absPath: string): string {
  return `/${relative(devDistDir, absPath).replaceAll("\\", "/")}`;
}

function formatBuildError(logs: Array<{ message?: string }>): string {
  return logs
    .map((log) => log.message ?? String(log))
    .join("\n\n");
}

async function buildDevBundle(): Promise<void> {
  rmSync(devDistDir, { recursive: true, force: true });
  mkdirSync(devDistDir, { recursive: true });

  const result = await Bun.build({
    entrypoints: ["./src/main.tsx"],
    outdir: devDistDir,
    splitting: true,
    sourcemap: "linked",
    target: "browser",
    publicPath: "/assets/",
    minify: false,
    plugins: [tailwindPlugin],
    naming: {
      entry: "assets/[name]-[hash].[ext]",
      chunk: "assets/[name]-[hash].[ext]",
      asset: "assets/[name]-[hash].[ext]",
    },
  });

  if (!result.success) {
    throw new Error(formatBuildError(result.logs));
  }

  const cssOutput = result.outputs.find((output) => output.path.endsWith(".css"));
  const jsEntry = result.outputs.find(
    (output) => output.kind === "entry-point" && output.path.endsWith(".js"),
  );

  const indexHtml = await Bun.file(indexHtmlPath).text();

  const outputHtml = indexHtml
    .replace(
      '<link rel="stylesheet" href="/src/index.css" />',
      cssOutput ? `<link rel="stylesheet" href="${relPath(cssOutput.path)}" />` : "",
    )
    .replace(
      '<script type="module" src="/src/main.tsx"></script>',
      jsEntry ? `<script type="module" src="${relPath(jsEntry.path)}"></script>` : "",
    );

  await Bun.write(resolve(devDistDir, "index.html"), outputHtml);
}

async function runDevBuild(reason: string): Promise<void> {
  try {
    await buildDevBundle();
    latestBuildError = "";
    console.log(`Dev bundle updated (${reason})`);
  } catch (errBuildDevBundle) {
    latestBuildError = errBuildDevBundle instanceof Error
      ? errBuildDevBundle.message
      : String(errBuildDevBundle);
    console.error(`Dev build failed (${reason})`);
    console.error(latestBuildError);
  }
}

function queueDevBuild(reason: string): void {
  if (rebuildTimer) {
    clearTimeout(rebuildTimer);
  }

  rebuildTimer = setTimeout(() => {
    buildQueue = buildQueue.then(() => runDevBuild(reason));
  }, 75);
}

function resolveServedFilePath(pathname: string): string | null {
  const requestPath = pathname === "/" ? "/index.html" : pathname;
  const filePath = resolve(devDistDir, `.${requestPath}`);
  const devRootPrefix = `${devDistDir}/`;
  if (filePath !== devDistDir && !filePath.startsWith(devRootPrefix)) {
    return null;
  }
  return filePath;
}

watch(srcDir, { recursive: true }, (_event, filename) => {
  if (!filename) {
    return;
  }

  if (
    filename.endsWith(".css") ||
    filename.endsWith(".ts") ||
    filename.endsWith(".tsx")
  ) {
    queueDevBuild(`src/${filename}`);
  }
});

watch(indexHtmlPath, () => {
  queueDevBuild("index.html");
});

async function proxyToAPI(
  request: Request,
  pathname: string,
): Promise<Response> {
  const url = new URL(pathname + new URL(request.url).search, API_TARGET);

  try {
    const proxyResponse = await fetch(url.toString(), {
      method: request.method,
      headers: request.headers,
      body:
        request.method !== "GET" && request.method !== "HEAD"
          ? await request.blob()
          : undefined,
    });

    return new Response(proxyResponse.body, {
      status: proxyResponse.status,
      statusText: proxyResponse.statusText,
      headers: proxyResponse.headers,
    });
  } catch {
    return new Response("API server unavailable", { status: 502 });
  }
}

await buildDevBundle();

const server = Bun.serve({
  port: DEV_PORT,
  async fetch(request) {
    const url = new URL(request.url);
    const pathname = url.pathname;

    if (pathname.startsWith("/api/")) {
      return proxyToAPI(request, pathname);
    }

    if (latestBuildError) {
      return new Response(latestBuildError, {
        status: 500,
        headers: { "Content-Type": "text/plain; charset=utf-8" },
      });
    }

    const filePath = resolveServedFilePath(pathname);
    if (filePath && existsSync(filePath)) {
      return new Response(Bun.file(filePath));
    }

    if (!pathname.includes(".")) {
      return new Response(Bun.file(resolve(devDistDir, "index.html")));
    }

    return new Response("Not found", { status: 404 });
  },
});

console.log(`Dev server running at http://127.0.0.1:${server.port}`);
console.log(`Proxying /api/* to ${API_TARGET}`);
console.log(`Serving bundled assets from ${devDistDir}`);
