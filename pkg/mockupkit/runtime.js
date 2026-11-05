(function(){
  function start(){
  document.body.classList.add('screens-ready');
  // Screens: one [data-screen] shown at a time; anything with data-go="id" shows that screen.
  function show(id){
    var screens=document.querySelectorAll('[data-screen]');if(!screens.length)return;
    var found=false;screens.forEach(function(s){var on=s.getAttribute('data-screen')===id;s.classList.toggle('is-shown',on);if(on)s.hidden=false;if(on)found=true;});
    if(!found){screens[0].classList.add('is-shown');screens[0].hidden=false;}
    document.querySelectorAll('[data-go]').forEach(function(a){a.classList.toggle('active',a.getAttribute('data-go')===id);});
    window.scrollTo(0,0);
  }
  var first=document.querySelector('[data-screen]');if(first)show(first.getAttribute('data-screen'));
  document.addEventListener('click',function(e){
    var go=e.target.closest&&e.target.closest('[data-go]');
    if(go){e.preventDefault();show(go.getAttribute('data-go'));return;}
    var tab=e.target.closest&&e.target.closest('[data-tabs] [data-tab]');
    if(tab){var box=tab.closest('[data-tabs]');var id=tab.getAttribute('data-tab');
      box.querySelectorAll('[data-tab]').forEach(function(t){t.setAttribute('aria-selected',t===tab?'true':'false');});
      box.querySelectorAll('[data-panel]').forEach(function(p){p.hidden=p.getAttribute('data-panel')!==id;});}
    var tog=e.target.closest&&e.target.closest('[data-toggle]');
    if(tog){var el=document.getElementById(tog.getAttribute('data-toggle'));if(el)el.hidden=!el.hidden;}
  });
  // Forms: submitting shows the form's .form-done message instead of sending anything.
  document.addEventListener('submit',function(e){var f=e.target;if(!(f instanceof HTMLFormElement))return;e.preventDefault();f.classList.add('is-done');f.querySelectorAll('.form-done').forEach(function(d){d.hidden=false;});});
  // Tabs start on their first tab.
  document.querySelectorAll('[data-tabs]').forEach(function(box){var t=box.querySelector('[data-tab]');if(t)t.click();});
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start);else start();
})();
