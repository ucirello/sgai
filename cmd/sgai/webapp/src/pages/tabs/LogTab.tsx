import { useEffect, useRef } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { ApiLogEntry } from "@/types";

interface LogTabProps {
  lines?: ApiLogEntry[];
}

function LogLine({ line }: { line: ApiLogEntry }) {
  return (
    <div className="font-mono text-xs leading-5 whitespace-pre-wrap break-all">
      {line.prefix && <span className="text-muted-foreground select-none">{line.prefix}</span>}
      <span>{line.text}</span>
    </div>
  );
}

export function LogTab({ lines = [] }: LogTabProps) {
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines]);

  if (lines.length === 0) {
    return <p className="text-sm italic text-muted-foreground">No logs available</p>;
  }

  return (
    <ScrollArea ref={scrollRef} className="max-h-[calc(100vh-16rem)] bg-muted/20 rounded-lg p-3 [&::-webkit-scrollbar]:hidden [scrollbar-width:none]">
      <div id="log-lines">
        {lines.map((line, index) => (
          <LogLine key={index} line={line} />
        ))}
      </div>
    </ScrollArea>
  );
}
