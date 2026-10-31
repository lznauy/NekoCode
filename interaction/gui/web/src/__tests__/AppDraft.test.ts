import {act,render,screen,fireEvent,waitFor} from '@testing-library/react';
import {createElement,forwardRef} from 'react';
import {it,expect,vi} from 'vitest';
import App from '../App';
const state=vi.hoisted(()=>({listeners:new Map<string,Set<Function>>(),load:vi.fn(),follow:vi.fn()}));
function emit(name:string,payload:unknown){for(const cb of state.listeners.get(name)??[])cb(payload)}
vi.mock('../lib/wails.ts',()=>({
 safeEventsOn:(e:string,f:Function)=>{if(!state.listeners.has(e))state.listeners.set(e,new Set());state.listeners.get(e)!.add(f);return()=>state.listeners.get(e)?.delete(f)},
 safeListSessions:async()=>[],safeNewSession:async()=>{for(const cb of state.listeners.get('session:changed')??[])cb({id:'draft',reason:'new',changedId:'draft',currentChanged:true,messages:[]});return{id:'draft'}},safeLoadSession:(id:string)=>state.load(id),safeDeleteSession:vi.fn(),
 safeGetConfig:async()=>null,safeSkillManagementView:async()=>null,safeCurrentModel:async()=>'',safeCommandMenu:async()=>null,
 safeSendMessage:async()=>{},safeAbort:vi.fn(),safeDeleteClipboardImage:async()=>{},safeSaveClipboardImage:vi.fn(),safeClearSelectedSkill:vi.fn(),safeContextSnapshot:vi.fn(),safeQuit:vi.fn(),safeSelectSkill:vi.fn(),safeSwitchModel:vi.fn()
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
it('new in-memory draft must not be resumed from disk',async()=>{
state.load.mockRejectedValue(new Error('open draft/session.json: no such file or directory'));
render(createElement(App));await act(async()=>{});
fireEvent.click(screen.getByRole('button',{name:'Create'}));
await act(async()=>{});
expect(state.load).not.toHaveBeenCalled();
expect(screen.queryByRole('alert')).toBeNull();
});
