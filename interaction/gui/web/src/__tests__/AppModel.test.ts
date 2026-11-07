import {act,render,screen,fireEvent,waitFor} from '@testing-library/react';
import {createElement,forwardRef} from 'react';
import {it,expect,vi} from 'vitest';
import App from '../App';
const state=vi.hoisted(()=>({listeners:new Map<string,Set<Function>>(),load:vi.fn(),follow:vi.fn(),model:'p|old'}));
function emit(name:string,payload:unknown){for(const cb of state.listeners.get(name)??[])cb(payload)}
vi.mock('../lib/wails.ts',()=>({
 safeEventsOn:(e:string,f:Function)=>{if(!state.listeners.has(e))state.listeners.set(e,new Set());state.listeners.get(e)!.add(f);return()=>state.listeners.get(e)?.delete(f)},
 safeListSessions:async()=>[],safeNewSession:async()=>{for(const cb of state.listeners.get('session:changed')??[])cb({id:'draft',reason:'new',changedId:'draft',currentChanged:true,messages:[]});return{id:'draft'}},safeLoadSession:(id:string)=>state.load(id),safeDeleteSession:vi.fn(),
 safeInteractionMode:async()=>null,safeSetInteractionMode:vi.fn(),safeGetConfig:async()=>null,safeSkillManagementView:async()=>null,safeCurrentModel:async()=>state.model,safeCommandMenu:async()=>null,
 safeSendMessage:async()=>{state.model='p|new'},safeAbort:vi.fn(),safeDeleteClipboardImage:async()=>{},safeSaveClipboardImage:vi.fn(),safeClearSelectedSkill:vi.fn(),safeContextSnapshot:vi.fn(),safeQuit:vi.fn(),safeSelectSkill:vi.fn(),safeSwitchModel:vi.fn()
}));
vi.mock('../hooks/useTheme.ts',()=>({useTheme:()=>({theme:'dark',toggle:vi.fn()})}));
vi.mock('../hooks/useAutoScroll.ts',()=>({useAutoScroll:()=>({containerRef:{current:null},endRef:{current:null},follow:state.follow})}));
vi.mock('../components/session/index.ts',()=>({SessionSidebar:({onCreate}:any)=>createElement('button',{onClick:onCreate},'Create')}));
vi.mock('../components/TopBar.tsx',()=>({TopBar:({model}:any)=>createElement('span',{},'model:'+model)}));
vi.mock('../components/MessageList.tsx',()=>({MessageList:forwardRef(()=>null)}));
vi.mock('../components/EmptyState.tsx',()=>({EmptyState:()=>null}));
vi.mock('../components/ConfigPanel.tsx',()=>({ConfigPanel:()=>null}));
vi.mock('../components/ContextPanel.tsx',()=>({ContextPanel:()=>null}));
vi.mock('../components/SkillPanel.tsx',()=>({SkillPanel:()=>null}));
it('model command must refresh the visible current model',async()=>{
render(createElement(App));await waitFor(()=>expect(screen.getByText('model:p / old')).toBeTruthy());
const textbox=screen.getByRole('textbox');
fireEvent.change(textbox,{target:{value:'/model new'}});
await act(async()=>{});
fireEvent.keyDown(textbox,{key:'Enter',code:'Enter'});
await act(async()=>{});
act(()=>{emit('agent:system',{content:'Switched to new'});emit('agent:done',{output:'',error:''});emit('agent:status',{status:'idle'})});
await act(async()=>{});
expect(screen.queryByText('model:p / new')).not.toBeNull();
});
