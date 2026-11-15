import {
  Component,
  Directive,
  ElementRef,
  HostListener,
  Input,
  OnDestroy,
  ViewContainerRef,
  inject,
} from '@angular/core';
import { Overlay, OverlayRef } from '@angular/cdk/overlay';
import { ComponentPortal } from '@angular/cdk/portal';

let nextHintId = 0;

@Component({
  selector: 'app-sidebar-hint',
  standalone: true,
  host: { role: 'tooltip', '[id]': 'id' },
  template: '{{ text }}',
  styles: `
    :host {
      display: block;
      max-width: 240px;
      padding: 6px 10px;
      border: 1px solid var(--color-border-default);
      border-radius: var(--radius-md);
      background: var(--color-surface);
      color: var(--color-text-primary);
      font-size: var(--font-size-caption);
      box-shadow: var(--shadow-panel);
      overflow-wrap: anywhere;
      pointer-events: none;
    }
  `,
})
class SidebarHintComponent {
  @Input() text = '';
  @Input() id = '';
}

/** 圖示側欄的完整名稱提示；以 CDK 浮層避免被側欄捲動區裁切。 */
@Directive({ selector: '[appSidebarHint]', standalone: true })
export class SidebarHintDirective implements OnDestroy {
  private readonly overlay = inject(Overlay);
  private readonly element = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly view = inject(ViewContainerRef);
  private readonly hintId = `sidebar-hint-${nextHintId++}`;
  private overlayRef?: OverlayRef;
  private text = '';

  @Input()
  set appSidebarHint(value: string) {
    if (value !== this.text) this.hide();
    this.text = value;
  }

  @HostListener('mouseenter')
  @HostListener('focusin')
  show(): void {
    if (!this.text || this.overlayRef) return;
    const position = this.overlay
      .position()
      .flexibleConnectedTo(this.element)
      .withPositions([
        { originX: 'end', originY: 'center', overlayX: 'start', overlayY: 'center', offsetX: 10 },
      ])
      .withViewportMargin(8)
      .withPush(true);
    this.overlayRef = this.overlay.create({
      positionStrategy: position,
      scrollStrategy: this.overlay.scrollStrategies.reposition(),
    });
    const component = this.overlayRef.attach(new ComponentPortal(SidebarHintComponent, this.view));
    component.setInput('text', this.text);
    component.setInput('id', this.hintId);
    const current = this.element.nativeElement.getAttribute('aria-describedby');
    this.element.nativeElement.setAttribute(
      'aria-describedby',
      [current, this.hintId].filter(Boolean).join(' '),
    );
  }

  @HostListener('mouseleave')
  @HostListener('focusout')
  @HostListener('click')
  @HostListener('keydown.escape')
  hide(): void {
    this.overlayRef?.dispose();
    this.overlayRef = undefined;
    const element = this.element.nativeElement;
    const remaining = (element.getAttribute('aria-describedby') ?? '')
      .split(' ')
      .filter((id) => id && id !== this.hintId)
      .join(' ');
    if (remaining) element.setAttribute('aria-describedby', remaining);
    else element.removeAttribute('aria-describedby');
  }

  ngOnDestroy(): void {
    this.hide();
  }
}
