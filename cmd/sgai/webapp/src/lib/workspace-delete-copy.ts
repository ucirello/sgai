type WorkspaceDeletionCopyOptions = {
  workspaceLabel: string;
  isExternal?: boolean;
  isFork?: boolean;
};

export function getWorkspaceDeletionCopy({
  workspaceLabel,
  isExternal = false,
  isFork = false,
}: WorkspaceDeletionCopyOptions) {
  if (isExternal && !isFork) {
    return {
      triggerText: "Remove",
      triggerLabel: `Detach ${workspaceLabel}`,
      dialogTitle: "Detach workspace",
      dialogDescription: `This will remove '${workspaceLabel}' from the interface. The directory and its contents will NOT be deleted.`,
      confirmText: "Remove",
      pendingText: "Removing...",
    };
  }

  if (isFork) {
    return {
      triggerText: "Delete fork",
      triggerLabel: `Delete fork ${workspaceLabel}`,
      dialogTitle: "Delete fork",
      dialogDescription: `This will permanently delete '${workspaceLabel}' from disk. This action cannot be undone.`,
      confirmText: "Delete fork",
      pendingText: "Deleting...",
    };
  }

  return {
    triggerText: "Delete workspace",
    triggerLabel: `Delete workspace ${workspaceLabel}`,
    dialogTitle: "Delete workspace",
    dialogDescription: `This will permanently delete '${workspaceLabel}' from disk. This action cannot be undone.`,
    confirmText: "Delete workspace",
    pendingText: "Deleting...",
  };
}
