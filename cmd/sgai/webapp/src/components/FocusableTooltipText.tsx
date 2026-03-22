import type { ComponentProps, HTMLAttributes, ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

interface FocusableTooltipTextProps extends HTMLAttributes<HTMLElement> {
  as?: "span" | "div" | "code";
  content?: ReactNode;
  side?: ComponentProps<typeof TooltipContent>["side"];
  tooltipClassName?: string;
  children: ReactNode;
}

export function FocusableTooltipText({
  as = "span",
  content,
  side = "top",
  tooltipClassName,
  className,
  children,
  tabIndex,
  ...props
}: FocusableTooltipTextProps) {
  const Comp = as;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Comp
          tabIndex={tabIndex ?? 0}
          className={cn(
            "block max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
            className,
          )}
          {...props}
        >
          {children}
        </Comp>
      </TooltipTrigger>
      <TooltipContent side={side} className={tooltipClassName}>
        {content ?? children}
      </TooltipContent>
    </Tooltip>
  );
}
