import {act,render,renderHook,screen,fireEvent,waitFor} from '@testing-library/react';
import {createElement,forwardRef} from 'react';
import {it,expect,vi} from 'vitest';
import App from '../App';
const state=vi.hoisted(()=>({listeners:new Map<string,Set<Function>>(),load:vi.fn(),list:vi.fn().mockResolvedValue([]),select:vi.fn().mockResolvedValue(undefined),follow:vi.fn()}));
function emit(name:string,payload:unknown){for(const cb of state.listeners.get(name)??[])cb(payload)}
vi.mock('../lib/wails.ts',()=>({
 safeEventsOn:(e:string,f:Function)=>{if(!state.listeners.has(e))state.listeners.set(e,new Set());state.listeners.get(e)!.add(f);return()=>state.listeners.get(e)?.delete(f)},
 safeListSessions:()=>state.list(),safeNewSession:async()=>{for(const cb of state.listeners.get('session:changed')??[])cb({id:'draft',reason:'new',changedId:'draft',currentChanged:true,messages:[]});return{id:'draft'}},safeLoadSession:(id:string)=>state.load(id),safeDeleteSession:vi.fn(),
 safeInteractionMode:async()=>null,safeSetInteractionMode:vi.fn(),safeGetConfig:async()=>null,safeSkillManagementView:async()=>null,safeCurrentModel:async()=>'',safeCommandMenu:async()=>null,
 safeSendMessage:async()=>{},safeAbort:vi.fn(),safeDeleteClipboardImage:async()=>{},safeSaveClipboardImage:vi.fn(),safeClearSelectedSkill:vi.fn(),safeContextSnapshot:vi.fn(),safeQuit:vi.fn(),safeSelectSkill:()=>state.select(),safeSwitchModel:vi.fn()
}));
vi.mock('../hooks/useTheme.ts',()=>({useTheme:()=>({theme:'dark',toggle:vi.fn()})}));
vi.mock('../hooks/useAutoScroll.ts',()=>({useAutoScroll:()=>({containerRef:{current:null},endRef:{current:null},follow:state.follow})}));
vi.mock('../components/session/index.ts',()=>({SessionSidebar:({onCreate}:any)=>createElement('button',{onClick:onCreate},'Create')}));
vi.mock('../components/TopBar.tsx',()=>({TopBar:()=>null}));
vi.mock('../components/MessageList.tsx',()=>({MessageList:forwardRef(()=>null)}));
vi.mock('../components/EmptyState.tsx',()=>({EmptyState:()=>null}));
vi.mock('../components/ConfigPanel.tsx',()=>({ConfigPanel:()=>null}));
vi.mock('../components/ContextPanel.tsx',()=>({ContextPanel:()=>null}));
vi.mock('../components/SkillPanel.tsx',()=>({SkillPanel:()=>null}));
vi.mock('../components/InputBar.tsx',()=>({InputBar:({onSelectSkill,selectedSkill}:any)=>createElement('button',{onClick:()=>onSelectSkill('hunt')},'Selected:'+selectedSkill)}));
it('clears selected skill after new session resets backend skill',async()=>{
render(createElement(App));await act(async()=>{});
fireEvent.click(screen.getByRole('button',{name:'Selected:'}));
await act(async()=>{});
expect(screen.getByRole('button',{name:'Selected:hunt'})).toBeTruthy();
fireEvent.click(screen.getByRole('button',{name:'Create'}));
await act(async()=>{});
expect(screen.getByRole('button',{name:'Selected:'})).toBeTruthy();
});

it('ignores a skill selection acknowledgment from the previous session', async () => {
  let finish!: () => void;
  state.select.mockReturnValueOnce(new Promise<void>(resolve => { finish = resolve }));
  render(createElement(App)); await act(async () => {});
  fireEvent.click(screen.getByRole('button', {name:'Selected:'}));
  act(() => emit('session:changed', {id:'new-draft',reason:'new',currentChanged:true,messages:[]}));
  await act(async () => finish());
  expect(screen.getByRole('button', {name:'Selected:'})).toBeTruthy();
});
