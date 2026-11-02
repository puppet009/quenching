import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source=await readFile(new URL('../src/utils/windowGeometry.ts',import.meta.url),'utf8');
const js=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext}}).outputText;
const {fitWindow,constrainWindow,resizeWindow,raiseWindow,adaptWindowMode}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));

test('snapped windows fill the desktop when a sidebar or viewport reduction removes space for two windows',()=>{
  assert.equal(adaptWindowMode('left',684),'maximized');
  assert.equal(adaptWindowMode('right',460),'maximized');
  assert.equal(adaptWindowMode('left',1000),'left');
  assert.equal(adaptWindowMode('floating',684),'floating');
});

test('windows fit the actual desktop after opening the assistant or reducing viewport size',()=>{
  for(const bounds of [{width:1060,height:620},{width:740,height:480},{width:320,height:240}]) {
    const rect=constrainWindow({x:600,y:450,width:1000,height:680},bounds);
    assert.ok(rect.x>=0 && rect.y>=0);
    assert.ok(rect.x+rect.width<=bounds.width && rect.y+rect.height<=bounds.height);
    assert.ok(rect.width>0 && rect.height>0);
  }
});
test('opening cascaded windows cannot place their title bar or resize control off screen',()=>{
  const bounds={width:880,height:510};
  const rect=fitWindow({x:480,y:400,width:1020,height:680},bounds);
  assert.ok(rect.x+rect.width<=bounds.width);
  assert.ok(rect.y+rect.height<=bounds.height);
});
test('resizing from the west keeps the east edge fixed and obeys the minimum',()=>{
  const rect=resizeWindow({x:100,y:30,width:600,height:400},'w',550,0,{width:1100,height:650});
  assert.equal(rect.x+rect.width,700);
  assert.equal(rect.width,440);
});
test('resizing and dragging cannot cross the desktop boundary',()=>{
  const bounds={width:1000,height:620};
  const rect=resizeWindow({x:100,y:30,width:600,height:400},'se',1000,1000,bounds);
  assert.deepEqual(rect,{x:100,y:30,width:900,height:590});
  assert.deepEqual(constrainWindow({...rect,x:-200,y:-100},bounds),{x:0,y:0,width:900,height:590});
});
test('focusing an existing window raises it without losing or duplicating other windows',()=>{
  assert.deepEqual(raiseWindow(['server:mall','tool:logs'],'server:mall'),['tool:logs','server:mall']);
  assert.deepEqual(raiseWindow(['server:mall'],'tool:metrics'),['server:mall','tool:metrics']);
});
