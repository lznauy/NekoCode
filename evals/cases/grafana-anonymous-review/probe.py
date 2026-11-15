"""Execute original method bodies with controlled in-memory adapters.
This is not a full Grafana/SQL integration test. The Go bodies are extracted
from the pinned source, with only package-qualified type names adapted.
"""
import json,os,re,subprocess,sys,tempfile
from pathlib import Path

def method(text,name):
    start=text.index('func '+name)
    begin=text.index('{',start);depth=1;i=begin+1
    # These selected methods contain SQL literals but no braces in literals.
    while depth:
        if text[i]=='{':depth+=1
        elif text[i]=='}':depth-=1
        i+=1
    return text[start:i]

def run(repo):
    impl=(repo/'pkg/services/anonymous/anonimpl/impl.go').read_text()
    database=(repo/'pkg/services/anonymous/anonimpl/anonstore/database.go').read_text()
    cache=method(impl,'(a *AnonDeviceService) tagDeviceUI').replace('anonstore.Device','Device')
    create=method(database,'(s *AnonDBStore) CreateOrUpdateDevice').replace('sqlstore.DBSession','DBSession')
    prefix='''package probe
import("context";"fmt";"net/http";"sync";"testing";"time")
var ErrDeviceLimitReached=fmt.Errorf("device limit reached")
const thirtyDays=30*24*time.Hour
var setting=struct{Env,Dev string}{"production","development"}
var migrator=struct{Postgres,MySQL,SQLite string}{"postgres","mysql","sqlite"}
type Device struct{DeviceID,ClientIP,UserAgent string;CreatedAt,UpdatedAt time.Time}
func(d *Device)CacheKey()string{return d.DeviceID}
type cacheStore struct{values map[string]any}
func(c *cacheStore)Get(k string)(any,bool){v,ok:=c.values[k];return v,ok}
func(c *cacheStore)SetDefault(k string,v any){c.values[k]=v}
type logger struct{}
func(logger)Debug(string,...any){}
type failingStore struct{calls int}
func(s *failingStore)CreateOrUpdateDevice(context.Context,*Device)error{s.calls++;return ErrDeviceLimitReached}
type AnonDeviceService struct{localCache *cacheStore;anonStore *failingStore;log logger}
type DBSession struct{db *fakeDB}
func(d *DBSession)Exec(...any)(any,error){d.db.mu.Lock();d.db.inserts++;d.db.mu.Unlock();return nil,nil}
type fakeDB struct{mu sync.Mutex;inserts int}
func(d *fakeDB)GetDBType()string{return "sqlite"}
func(d *fakeDB)WithDbSession(ctx context.Context,f func(*DBSession)error)error{return f(&DBSession{d})}
type AnonDBStore struct{sqlStore *fakeDB;deviceLimit int64;arrived chan struct{};release chan struct{}}
func(s *AnonDBStore)CountDevices(context.Context,time.Time,time.Time)(int64,error){s.arrived<-struct{}{};<-s.release;return 0,nil}
func(s *AnonDBStore)updateDevice(context.Context,*Device)error{return ErrDeviceLimitReached}
'''
    tests='''
func TestCacheFailureBypassObserved(t *testing.T){
 s:=&failingStore{};a:=&AnonDeviceService{localCache:&cacheStore{map[string]any{}},anonStore:s}
 d:=&Device{DeviceID:"new-device"}
 first:=a.tagDeviceUI(context.Background(),nil,d)
 second:=a.tagDeviceUI(context.Background(),nil,d)
 if first!=ErrDeviceLimitReached||second!=nil||s.calls!=1{t.Fatalf("unexpected: first=%v second=%v calls=%d",first,second,s.calls)}
 t.Log("Original body: first rejected; second returns success without storage retry")
}
func TestConcurrentCountThenInsertObserved(t *testing.T){
 db:=&fakeDB{};s:=&AnonDBStore{sqlStore:db,deviceLimit:1,arrived:make(chan struct{},2),release:make(chan struct{})}
 var wg sync.WaitGroup
 for _,id:=range []string{"one","two"}{wg.Add(1);go func(id string){defer wg.Done();if err:=s.CreateOrUpdateDevice(context.Background(),&Device{DeviceID:id});err!=nil{t.Error(err)}}(id)}
 <-s.arrived;<-s.arrived;close(s.release);wg.Wait()
 if db.inserts!=2{t.Fatalf("inserts=%d",db.inserts)}
 t.Log("Original body: both count zero, both insert despite limit 1")
}
'''
    with tempfile.TemporaryDirectory(prefix='neko-grafana-probe-') as temp:
        root=Path(temp);(root/'go.mod').write_text('module grafana-review-probe\n\ngo 1.24.0\n')
        (root/'probe_test.go').write_text(prefix+cache+'\n'+create+'\n'+tests)
        result=subprocess.run(['go','test','-v','-count=1','-timeout','20s','.'],cwd=root,env=dict(os.environ,GOWORK='off'),capture_output=True,text=True,timeout=60)
    return {'returncode':result.returncode,'stdout':result.stdout,'stderr':result.stderr,'scope':'Original Go method bodies, controlled in-memory adapters; not full SQL or authentication integration'}

if __name__=='__main__':
    result=run(Path(sys.argv[1]));print(json.dumps(result,ensure_ascii=False,indent=2));sys.exit(result['returncode'])
