import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';

interface SkillWorkspaceValue {
  projectId: string;
  runId: string;
  unlocked: boolean;
  epoch: number;
  setProjectId: (id: string) => void;
  setRunId: (id: string) => void;
  markUnlocked: () => void;
  lock: () => Promise<void>;
}

const SkillWorkspaceContext = createContext<SkillWorkspaceValue | null>(null);

export function SkillWorkspaceProvider({ children, projectId: currentProject = '' }: { children: ReactNode; projectId?: string }) {
  const queryClient = useQueryClient();
  const [projectId, setProjectIdState] = useState(currentProject);
  const [runId, setRunIdState] = useState('');
  const [unlocked, setUnlocked] = useState(true);
  const [epoch, setEpoch] = useState(0);

  const setProjectId = (id: string) => {
    setProjectIdState((previous) => {
      if (previous === id) return previous;
      if (previous) {
        void queryClient.removeQueries({ queryKey: ['vu-project', previous] });
        void queryClient.removeQueries({ queryKey: ['vu-visible', previous] });
        void queryClient.removeQueries({ queryKey: ['vu-runs', previous] });
        void queryClient.removeQueries({ queryKey: ['vu-run', previous] });
        void queryClient.removeQueries({ queryKey: ['vu-context', previous] });
      }
      setRunIdState('');
      setEpoch((value) => value + 1);
      return id;
    });
  };

  const setRunId = (id: string) => {
    setRunIdState((previous) => {
      if (previous === id) return previous;
      setEpoch((value) => value + 1);
      return id;
    });
  };

  const lock = async () => {
    document.querySelectorAll<HTMLMediaElement>('video,audio').forEach(node => node.pause());
    setUnlocked(false);
    setProjectIdState('');
    setRunIdState('');
    setEpoch((value) => value + 1);
  };

  useEffect(() => {
    document.querySelectorAll('video,audio').forEach((node) => {
      if (node instanceof HTMLMediaElement) node.pause();
    });
  }, [epoch]);

  const value = useMemo<SkillWorkspaceValue>(() => ({
    projectId, runId, unlocked, epoch, setProjectId, setRunId, markUnlocked: () => setUnlocked(true), lock
  }), [projectId, runId, unlocked, epoch]);

  return <SkillWorkspaceContext.Provider value={value}>{children}</SkillWorkspaceContext.Provider>;
}

export function useSkillWorkspace(): SkillWorkspaceValue {
  const value = useContext(SkillWorkspaceContext);
  if (!value) throw new Error('Skill 工作区尚未挂载');
  return value;
}
