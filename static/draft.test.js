import { test, expect } from "bun:test";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

function harness() {
 const values = new Map();
 const localStorage={getItem:k=>values.get(k)??null,setItem:(k,v)=>values.set(k,v),removeItem:k=>values.delete(k)};
 const scope={window:{localStorage,addEventListener(){}},document:{addEventListener(){},documentElement:{}},MutationObserver:class {observe(){}},Event:class {},HTMLFormElement:class {}};
 runInNewContext(readFileSync(new URL("./draft.js",import.meta.url),"utf8"),scope);
 const form=(owner,date,entry,token,version="0")=>{
  const tokenField={name:"token",type:"hidden",value:token};const service={name:"services",type:"text",value:"100",dispatchEvent(){}};
  const elements=[tokenField,service];elements.namedItem=name=>elements.find(e=>e.name===name);
  return {dataset:{draftUser:owner,draft:date,draftEntry:entry,draftToken:token,version},elements};
 };
 return {scope,values,form};
}

test("a reload restores the stable create token with typed amounts",()=>{
 const h=harness();const first=h.form("olga","2026-11-20","new","original-token");h.scope.save(first);
 const reload=h.form("olga","2026-11-20","new","original-token");reload.elements[1].value="";h.scope.restore(reload);
 expect(reload.elements[0].value).toBe("original-token");expect(reload.elements[1].value).toBe("100");
});
test("successful acknowledgement clears only the matching owner day create token",()=>{
 const h=harness();for (const f of [h.form("olga","2026-11-20","new","matching"),h.form("maria","2026-11-20","new","other"),h.form("olga","2026-11-21","new","tomorrow")])h.scope.save(f);
 const saved=h.form("olga","2026-11-20","saved-id","matching","1");saved.dataset.draftAck="wrong";h.scope.acknowledge(saved);expect(h.values.size).toBe(3);
 saved.dataset.draftAck="matching";h.scope.acknowledge(saved);expect(h.values.size).toBe(2);
});
test("edit version drift drops only that entry draft and leaves create and legacy",()=>{
 const h=harness();const edit=h.form("olga","2026-11-20","entry-id","token","1");h.scope.save(edit);h.scope.save(h.form("olga","2026-11-20","new","new-token"));h.scope.save(h.form("olga","2026-11-20",undefined,undefined));edit.dataset.version="2";h.scope.restore(edit);expect(h.values.size).toBe(2);
});

test("independent same-day forms keep separate create identities and acknowledgement",()=>{
 const h=harness();const a=h.form("olga","2026-11-20","new","token-a");const b=h.form("olga","2026-11-20","new","token-b");h.scope.save(a);h.scope.restore(b);h.scope.save(b);
 expect(b.elements[0].value).toBe("token-b");expect(h.values.size).toBe(2);
 const reload=h.form("olga","2026-11-20","new","token-a");reload.elements[1].value="";h.scope.restore(reload);expect(reload.elements[1].value).toBe("100");
 const saved=h.form("olga","2026-11-20","saved-a","token-a","1");saved.dataset.draftAck="token-a";h.scope.acknowledge(saved);expect(h.values.size).toBe(1);expect([...h.values.keys()][0]).toContain("token-b");
});

test("restoring an optional draft opens its sales or certificate section",()=>{
 const source=readFileSync(new URL("../internal/views/clients.templ",import.meta.url),"utf8");
 const extrasEffect=source.match(/const clientExtrasOpen = `([^`]*)`/s)?.[1];expect(extrasEffect).toBeDefined();
 const certificateEffect=source.match(/const clientCertificateOpen = `([^`]*)`/s)?.[1];expect(certificateEffect).toBeDefined();
 for(const name of ["creams","oil","certificates","certificatePaid"]){
  const effect=name==="certificatePaid"?certificateEffect:extrasEffect;
  const otherEffect=name==="certificatePaid"?extrasEffect:certificateEffect;
  const h=harness();const saved=h.form("olga","2026-11-20","new","same-token");saved.elements.push({name,type:"text",value:"20",dispatchEvent(){}});h.scope.save(saved);
  const details={open:false};const signals={creams:"",oil:"",certificates:"",certificatePaid:""};const runtime={$_client:signals,el:details};runInNewContext(effect,runtime);expect(details.open).toBe(false);
  const reload=h.form("olga","2026-11-20","new","same-token");reload.elements.push({name,type:"text",value:"",dispatchEvent(){signals[name]=this.value;runInNewContext(effect,runtime)}});h.scope.restore(reload);
  expect(details.open).toBe(true);expect(reload.elements.namedItem(name).value).toBe("20");
  const otherDetails={open:false};runInNewContext(otherEffect,{$_client:signals,el:otherDetails});expect(otherDetails.open).toBe(false);
 }
});

test("keyboard Next follows visible fields and skips collapsed extras",()=>{
 const source=readFileSync(new URL("../internal/views/report.templ",import.meta.url),"utf8");const action=source.match(/const nextField = `([^`]*)`/s)?.[1];expect(action).toBeDefined();
 for(const [open,hiddenCream,wanted] of [[false,false,"serviceCash"],[true,false,"creams"],[true,true,"oil"]]){
  let focused="services";let prevented=false;
  const fields=["services","creams","oil","certificates","certificatePaid","serviceCash","serviceCard"].map(name=>({name,enterKeyHint:"next",closest(){return !open&&["creams","oil","certificates","certificatePaid"].includes(name)?{}:null},getClientRects(){return hiddenCream&&name==="creams"?[]:[{}]},focus(){focused=name}}));
  const evt={key:"Enter",target:fields[0],preventDefault(){prevented=true}};const el={querySelectorAll(){return fields}};runInNewContext(action,{evt,el});expect(prevented).toBe(true);expect(focused).toBe(wanted);
 }
});
